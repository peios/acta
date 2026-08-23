// Package board owns the board domain: user-defined statuses (lanes) and the
// items within them. It sits between the HTTP handlers and the store, holding
// the rules that aren't pure persistence — input validation, the
// delete-a-lane-only-when-empty guard, default-lane seeding, and the
// move/transition logic that keeps each lane's positions dense.
package board

import (
	"context"
	"errors"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/peios/acta/internal/identity"
	"github.com/peios/acta/internal/store"
)

const (
	MaxStatusNameLen  = 40
	MaxItemTitleLen   = 200
	MaxDescriptionLen = 1_000_000
	MaxCommentLen     = 5000
	// Documents hold whole reports, so the body ceiling is generous (~4MB of
	// text — well above any markdown a human or agent writes, but a backstop
	// against a pasted blob). The title is a one-liner.
	MaxDocumentLen      = 4_000_000
	MaxDocumentTitleLen = 200

	// endOfLane is a large index that MoveItem clamps to a lane's end; used by
	// status changes that carry no explicit position.
	endOfLane = 1 << 30
)

var (
	ErrInvalidName        = errors.New("board: invalid status name")
	ErrInvalidTitle       = errors.New("board: invalid item title")
	ErrInvalidDescription = errors.New("board: description too long")
	ErrInvalidComment     = errors.New("board: invalid comment")
	ErrCommentForbidden   = errors.New("board: not the comment author")
	ErrInvalidDocument    = errors.New("board: invalid document")
	ErrStatusNotEmpty     = errors.New("board: status still has items")
	ErrNoStatus           = errors.New("board: workspace has no statuses")
	ErrCycle              = errors.New("board: would create a cycle")
	// ErrStatusMismatch is returned when a status doesn't belong to the
	// workspace it's being used in — a malformed or cross-workspace request.
	ErrStatusMismatch = errors.New("board: status not in this workspace")
	// ErrInvalidColor is returned for a lane colour that isn't "" (auto) or a
	// member of Palette — guarding the value that reaches an inline style.
	ErrInvalidColor = errors.New("board: invalid lane colour")
)

// DefaultStatuses are the lanes seeded onto a new workspace's Tasks board so it
// is usable immediately. DefaultBacklogStatuses does the same for the Backlog
// board — a single entry lane, since most workspaces keep just the one.
var (
	DefaultStatuses        = []string{"To do", "Doing", "Done"}
	DefaultBacklogStatuses = []string{"Backlog"}
)

// Palette is the set of lane colours the board offers. A status without an
// explicit colour falls back to one of these by position, so a fresh board is
// colourful with no configuration; picking a swatch pins an explicit choice.
// Soft pastels including a red/amber/green traffic-light trio; ordered so the
// default To do / Doing / Done seed reads as unstarted-slate → active-blue →
// done-green.
var Palette = []string{
	"#9aa6b8", // slate
	"#7cb8e8", // sky
	"#86c98f", // green
	"#e8b96d", // amber
	"#e08585", // red
	"#b79fe6", // lavender
	"#e394b5", // pink
	"#79d2c4", // teal
}

// ColorFor returns a status's display colour: its explicit Color when set,
// otherwise a stable palette colour derived from its board position.
func ColorFor(s store.Status) string {
	if s.Color != "" {
		return s.Color
	}
	return Palette[((s.Position%len(Palette))+len(Palette))%len(Palette)]
}

// eventCoalesceWindow is how long an autosave-driven verb (e.g. a description
// edit, saved every few hundred ms while typing) stays quiet in the activity
// log after its first entry. One editing session logs once, per item per actor,
// rather than once per keystroke-debounced save.
const eventCoalesceWindow = 5 * time.Minute

// coalescingVerbs are the verbs subject to the window above: those a client
// emits repeatedly during a single edit (the autosaved text fields). Discrete
// actions (status moves, assignments, comments, archives) are never coalesced —
// each is its own entry.
var coalescingVerbs = map[string]bool{
	store.EventItemDescribed: true,
	store.EventItemRenamed:   true,
}

// mergeCoalesced folds a later edit's data into the entry that opened the burst.
// A rename keeps the burst's original "from" and adopts the latest "to", so the
// single entry reads original → final rather than freezing mid-keystroke; other
// verbs just carry the latest data forward.
func mergeCoalesced(verb string, prev, next map[string]string) map[string]string {
	if verb == store.EventItemRenamed {
		return map[string]string{"from": prev["from"], "to": next["to"]}
	}
	return next
}

type Service struct {
	store     store.Store
	now       func() time.Time
	notifiers []Notifier
	// snapshots rate-limits the write-on-read progress snapshot (see
	// EnsureSnapshot) so read paths can call it without measuring every render.
	snapshots snapshotGuard
}

// Notifier is an out-of-band delivery channel for notifications (e.g. Web Push,
// the live SSE bell). The board calls every attached channel as a best-effort
// side-effect when it files a notification — the inbox row is the durable
// record, so with no Notifier attached (the default) notifications are simply
// in-app only. Implementations must not block: NotifyUser is invoked on the
// request path.
type Notifier interface {
	NotifyUser(ctx context.Context, userID string, n store.Notification)
}

// notify delivers n to every attached channel, best-effort. This is the single
// fan-out point for out-of-band delivery, shared by mentions and subscriptions.
func (s *Service) notify(ctx context.Context, userID string, n store.Notification) {
	for _, nf := range s.notifiers {
		nf.NotifyUser(ctx, userID, n)
	}
}

// Option configures a Service.
type Option func(*Service)

// WithClock overrides the time source used to stamp and coalesce activity
// events. Tests inject a controllable clock; production uses time.Now.
func WithClock(now func() time.Time) Option {
	return func(s *Service) { s.now = now }
}

// WithNotifier attaches an out-of-band notification channel (Web Push, live
// SSE). It may be passed more than once to attach several; with none,
// notifications are delivered in-app only.
func WithNotifier(n Notifier) Option {
	return func(s *Service) { s.notifiers = append(s.notifiers, n) }
}

// AddNotifier attaches a Notifier after construction — for a channel that only
// exists once a later layer is built (the web SSE bell, whose hub the handler
// owns). Call it during startup, before serving; it is not safe against a
// concurrent notify.
func (s *Service) AddNotifier(n Notifier) {
	s.notifiers = append(s.notifiers, n)
}

func New(st store.Store, opts ...Option) *Service {
	s := &Service{store: st, now: time.Now}
	for _, o := range opts {
		o(s)
	}
	return s
}

// recordEvent appends an entry to the activity log, attributing it to the
// principal carried by ctx (recorded as a system action when none is present).
// Recording is best-effort observability: a failure is logged but never fails
// the mutation it describes, and it is intentionally a separate write — the log
// is history, not part of the operation's consistency.
func (s *Service) recordEvent(ctx context.Context, item store.Item, verb string, data map[string]string) {
	s.recordEventExcluding(ctx, item, verb, data, nil)
}

// recordEventExcluding is recordEvent with a set of subscriber ids to skip in the
// fanout — used by AddComment so an @mention (filed separately, more specific)
// wins over the generic comment-activity notification for the same recipient.
func (s *Service) recordEventExcluding(ctx context.Context, item store.Item, verb string, data map[string]string, exclude map[string]bool) {
	ev := store.Event{
		WorkspaceID: item.WorkspaceID,
		ItemID:      item.ID,
		ItemTitle:   item.Title,
		Verb:        verb,
		Data:        data,
		CreatedAt:   s.now(),
	}
	// An item's board is derived from its status, so resolve it here for the
	// per-board activity feed. Callers that change an item's status set the new
	// StatusID on item before recording, so a board-crossing move is attributed
	// to the destination board. Best-effort: an unresolved status leaves it "".
	if st, err := s.store.StatusByID(ctx, item.StatusID); err == nil {
		ev.BoardID = st.BoardID
	}
	if p, ok := identity.FromContext(ctx); ok && p != nil {
		ev.ActorID = p.ID
		ev.ActorName = principalName(p)
	}
	// Coalesce autosave-driven verbs: the first edit of a burst opens an entry,
	// and later edits within the window fold into it (advancing its time and
	// carrying the latest data) rather than each adding a row. Scoped per item
	// per actor, so a different item or person opens its own entry. Any store
	// error falls through to a plain record — better a duplicate than a lost one.
	if coalescingVerbs[verb] {
		since := ev.CreatedAt.Add(-eventCoalesceWindow)
		prev, ok, err := s.store.LatestEventForActor(ctx, item.ID, ev.ActorID, verb, since)
		switch {
		case err != nil:
			slog.Error("coalesce lookup", "verb", verb, "item", item.ID, "err", err)
		case ok:
			merged := mergeCoalesced(verb, prev.Data, ev.Data)
			if err := s.store.TouchEvent(ctx, prev.ID, ev.CreatedAt, merged); err != nil {
				slog.Error("coalesce touch", "verb", verb, "item", item.ID, "err", err)
				break // fall through to record a fresh entry
			}
			return
		}
	}
	if _, err := s.store.RecordEvent(ctx, ev); err != nil {
		slog.Error("record activity event", "verb", verb, "item", item.ID, "err", err)
		return
	}
	// Fan the freshly-logged event out to its subscribers. Deliberately only on
	// the RecordEvent path, not the coalesce fold above — a burst of autosave
	// edits notifies once (when it opens an entry), not per keystroke.
	s.notifySubscribers(ctx, ev, item, exclude)
}

func principalName(p *identity.Principal) string {
	if p.Display != "" {
		return p.Display
	}
	return p.Username
}

// userName resolves a user id to a display name for an event payload, returning
// "" for the empty id (e.g. an unassignment).
func (s *Service) userName(ctx context.Context, userID string) string {
	if userID == "" {
		return ""
	}
	u, err := s.store.UserByID(ctx, userID)
	if err != nil {
		return ""
	}
	if u.Display != "" {
		return u.Display
	}
	return u.Username
}

// excerpt collapses body to a single short line for an event payload.
func excerpt(body string, max int) string {
	s := strings.Join(strings.Fields(body), " ")
	if len([]rune(s)) > max {
		s = string([]rune(s)[:max]) + "…"
	}
	return s
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// --- boards ---

// Boards returns a workspace's boards in display order (Tasks, then Backlog).
func (s *Service) Boards(ctx context.Context, workspaceID string) ([]store.Board, error) {
	return s.store.BoardsByWorkspace(ctx, workspaceID)
}

// DefaultBoard is a workspace's primary board — the first by position (Tasks).
// It's what a board-unaware caller (or the bare /{slug} URL) resolves to.
func (s *Service) DefaultBoard(ctx context.Context, workspaceID string) (store.Board, error) {
	boards, err := s.store.BoardsByWorkspace(ctx, workspaceID)
	if err != nil {
		return store.Board{}, err
	}
	if len(boards) == 0 {
		return store.Board{}, store.ErrBoardNotFound
	}
	return boards[0], nil
}

// BoardBySlug resolves a board within a workspace by its URL slug.
func (s *Service) BoardBySlug(ctx context.Context, workspaceID, slug string) (store.Board, error) {
	return s.store.BoardBySlug(ctx, workspaceID, slug)
}

// BoardByID resolves a board by id.
func (s *Service) BoardByID(ctx context.Context, id string) (store.Board, error) {
	return s.store.BoardByID(ctx, id)
}

// EntryStatus returns a board's entry lane — where new (and cross-board) items
// land. It falls back to the lowest-position lane if no lane is flagged (an
// invariant the seed and the entry-management guard keep, but a safe default).
func (s *Service) EntryStatus(ctx context.Context, boardID string) (store.Status, error) {
	lanes, err := s.store.StatusesByBoard(ctx, boardID)
	if err != nil {
		return store.Status{}, err
	}
	if len(lanes) == 0 {
		return store.Status{}, ErrNoStatus
	}
	for _, l := range lanes {
		if l.IsEntry {
			return l, nil
		}
	}
	return lanes[0], nil
}

// --- statuses ---

func (s *Service) Statuses(ctx context.Context, workspaceID string) ([]store.Status, error) {
	return s.store.StatusesByWorkspace(ctx, workspaceID)
}

func (s *Service) StatusByID(ctx context.Context, id string) (store.Status, error) {
	return s.store.StatusByID(ctx, id)
}

// BoardStatuses returns one board's lanes, in position order.
func (s *Service) BoardStatuses(ctx context.Context, boardID string) ([]store.Status, error) {
	return s.store.StatusesByBoard(ctx, boardID)
}

// CreateStatus appends a lane to a specific board. The caller resolves which
// board the request targets (the board the user is viewing).
func (s *Service) CreateStatus(ctx context.Context, boardID, name string) (store.Status, error) {
	name, err := cleanName(name)
	if err != nil {
		return store.Status{}, err
	}
	b, err := s.store.BoardByID(ctx, boardID)
	if err != nil {
		return store.Status{}, err
	}
	existing, err := s.store.StatusesByBoard(ctx, b.ID)
	if err != nil {
		return store.Status{}, err
	}
	return s.store.CreateStatus(ctx, store.Status{
		WorkspaceID: b.WorkspaceID,
		BoardID:     b.ID,
		Name:        name,
		Position:    len(existing),
	})
}

func (s *Service) RenameStatus(ctx context.Context, id, name string) error {
	name, err := cleanName(name)
	if err != nil {
		return err
	}
	return s.store.RenameStatus(ctx, id, name)
}

// SetStatusColor pins a lane's colour to a palette entry, or clears it to ""
// (auto). Anything else is rejected so only known-safe values reach the UI.
func (s *Service) SetStatusColor(ctx context.Context, id, color string) error {
	color, err := cleanColor(color)
	if err != nil {
		return err
	}
	return s.store.SetStatusColor(ctx, id, color)
}

// cleanColor canonicalises a colour: "" passes through (auto), a palette member
// is normalised to its canonical casing, anything else is ErrInvalidColor.
func cleanColor(c string) (string, error) {
	if c == "" {
		return "", nil
	}
	for _, p := range Palette {
		if strings.EqualFold(c, p) {
			return p, nil
		}
	}
	return "", ErrInvalidColor
}

func (s *Service) ReorderStatuses(ctx context.Context, workspaceID string, orderedIDs []string) error {
	return s.store.ReorderStatuses(ctx, workspaceID, orderedIDs)
}

// DeleteStatus refuses to remove a lane that still holds items — the user
// empties it first. This keeps deletion from silently dropping work.
func (s *Service) DeleteStatus(ctx context.Context, id string) error {
	items, err := s.store.ItemsByStatus(ctx, id)
	if err != nil {
		return err
	}
	if len(items) > 0 {
		return ErrStatusNotEmpty
	}
	return s.store.DeleteStatus(ctx, id)
}

// --- items ---

func (s *Service) Items(ctx context.Context, workspaceID string) ([]store.Item, error) {
	return s.store.ItemsByWorkspace(ctx, workspaceID)
}

// ItemsWithSubtasks returns every active item — top-level and nested — ordered by
// position, for the board's "Show sub-tasks" view (which surfaces children as
// their own cards). It reuses AllItemsByWorkspace and re-sorts, since that query
// orders by title (for the reparent picker) rather than board position.
func (s *Service) ItemsWithSubtasks(ctx context.Context, workspaceID string) ([]store.Item, error) {
	items, err := s.store.AllItemsByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Position < items[j].Position })
	return items, nil
}

// SearchItems returns items whose title or description contains query
// (case-insensitive substring) at every nesting depth, ranked title-matches-first
// then newest. boardID scopes to one board (an item's board is its status's);
// "" searches every board. includeArchived false omits archived items. It backs
// list_items' free-text `q`.
func (s *Service) SearchItems(ctx context.Context, workspaceID, boardID, query string, includeArchived bool) ([]store.Item, error) {
	return s.store.SearchItems(ctx, workspaceID, boardID, query, includeArchived)
}

func (s *Service) CreateItem(ctx context.Context, workspaceID, statusID, title string) (store.Item, error) {
	return s.createItem(ctx, workspaceID, statusID, title, "")
}

// createItem is the shared worker; createdBy records the authoring principal
// ("" when unknown, e.g. seeding or tests).
func (s *Service) createItem(ctx context.Context, workspaceID, statusID, title, createdBy string) (store.Item, error) {
	title, err := cleanTitle(title)
	if err != nil {
		return store.Item{}, err
	}
	if err := s.requireStatusInWorkspace(ctx, statusID, workspaceID); err != nil {
		return store.Item{}, err
	}
	lane, err := s.store.ItemsByStatus(ctx, statusID)
	if err != nil {
		return store.Item{}, err
	}
	it, err := s.store.CreateItem(ctx, store.Item{
		WorkspaceID: workspaceID,
		StatusID:    statusID,
		Title:       title,
		Position:    len(lane),
		CreatedBy:   createdBy,
	})
	if err != nil {
		return store.Item{}, err
	}
	status, _ := s.store.StatusByID(ctx, statusID)
	s.recordEvent(ctx, it, store.EventItemCreated, map[string]string{"status": status.Name})
	s.autoSubscribe(ctx, createdBy, store.SubjectItem, it.ID) // author watches what they create
	return it, nil
}

func (s *Service) RenameItem(ctx context.Context, id, title string) error {
	title, err := cleanTitle(title)
	if err != nil {
		return err
	}
	item, err := s.store.ItemByID(ctx, id)
	if err != nil {
		return err
	}
	if item.Title == title {
		return nil
	}
	if err := s.store.RenameItem(ctx, id, title); err != nil {
		return err
	}
	from := item.Title
	item.Title = title
	s.recordEvent(ctx, item, store.EventItemRenamed, map[string]string{"from": from, "to": title})
	return nil
}

// statusChangeData builds an item.status_changed event's payload. When the move
// crosses boards (the from/to statuses live on different boards), it records the
// destination board so the feed can say "moved to the Backlog board" rather than
// an ambiguous lane-to-lane line.
func (s *Service) statusChangeData(ctx context.Context, from, to store.Status) map[string]string {
	data := map[string]string{"from": from.Name, "to": to.Name}
	if from.BoardID != to.BoardID {
		if tb, err := s.store.BoardByID(ctx, to.BoardID); err == nil {
			data["toBoard"] = tb.Name
		}
	}
	return data
}

// MoveItem transitions an item into toStatusID at the given index, keeping both
// the destination lane and (if it changed) the source lane densely ordered.
func (s *Service) MoveItem(ctx context.Context, itemID, toStatusID string, index int) error {
	return s.moveItem(ctx, itemID, toStatusID, index, nil)
}

// moveItem is MoveItem with extra key/values merged into the status-change
// event's data (e.g. the facts confirmed to pass a checklist gate).
func (s *Service) moveItem(ctx context.Context, itemID, toStatusID string, index int, extra map[string]string) error {
	item, err := s.store.ItemByID(ctx, itemID)
	if err != nil {
		return err
	}
	if err := s.requireStatusInWorkspace(ctx, toStatusID, item.WorkspaceID); err != nil {
		return err
	}

	// Destination order: the lane's current items minus this one (a no-op when
	// it's a cross-lane move), with the item spliced in at index.
	dest, err := s.store.ItemsByStatus(ctx, toStatusID)
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(dest)+1)
	for _, it := range dest {
		if it.ID != itemID {
			ids = append(ids, it.ID)
		}
	}
	if index < 0 {
		index = 0
	}
	if index > len(ids) {
		index = len(ids)
	}
	ordered := make([]string, 0, len(ids)+1)
	ordered = append(ordered, ids[:index]...)
	ordered = append(ordered, itemID)
	ordered = append(ordered, ids[index:]...)
	if err := s.store.ReorderItems(ctx, toStatusID, ordered); err != nil {
		return err
	}

	// Re-densify the source lane if the item left it, and log the transition.
	if item.StatusID != toStatusID {
		src, err := s.store.ItemsByStatus(ctx, item.StatusID)
		if err != nil {
			return err
		}
		srcIDs := make([]string, len(src))
		for i, it := range src {
			srcIDs[i] = it.ID
		}
		if err := s.store.ReorderItems(ctx, item.StatusID, srcIDs); err != nil {
			return err
		}
		from, _ := s.store.StatusByID(ctx, item.StatusID)
		to, _ := s.store.StatusByID(ctx, toStatusID)
		item.StatusID = toStatusID
		s.recordEvent(ctx, item, store.EventItemStatusChange,
			mergeData(s.statusChangeData(ctx, from, to), extra))
	}
	return nil
}

func (s *Service) DeleteItem(ctx context.Context, id string) error {
	return s.store.DeleteItem(ctx, id)
}

// --- item detail ---

func (s *Service) Item(ctx context.Context, id string) (store.Item, error) {
	return s.store.ItemByID(ctx, id)
}

// ItemByRef resolves an item by its per-workspace ref number (the N in a human
// id like ACTA-12).
func (s *Service) ItemByRef(ctx context.Context, workspaceID string, refNum int) (store.Item, error) {
	return s.store.ItemByRef(ctx, workspaceID, refNum)
}

// Users lists every account, for the assignee picker (there's no membership
// model yet, so any user can be assigned).
func (s *Service) Users(ctx context.Context) ([]store.User, error) {
	return s.store.ListUsers(ctx)
}

// User resolves a single principal by id (used to paint an assignee avatar on a
// live card update).
func (s *Service) User(ctx context.Context, id string) (store.User, error) {
	return s.store.UserByID(ctx, id)
}

// Assignables returns the principals worth offering when the current actor
// directs work — every active human plus the actor's *own* agents, ordered
// (humans by display name, then own agents by handle). This is purely a UI
// declutter, not an authority boundary: the assign API and MCP stay permissive
// (an agent may still be pointed at another's agent), and you can still mention
// anyone by typing their full @handle. Both the assignee picker and the @-
// mention autocomplete draw from this one set.
func (s *Service) Assignables(ctx context.Context) ([]store.User, error) {
	all, err := s.store.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	me := ""
	if p, ok := identity.FromContext(ctx); ok && p != nil {
		me = p.ID
	}
	var humans, mine []store.User
	for _, u := range all {
		if u.DisabledAt != nil {
			continue
		}
		switch u.AgentOfID {
		case "":
			humans = append(humans, u)
		case me:
			mine = append(mine, u)
		}
	}
	sort.Slice(humans, func(i, j int) bool { return displayKey(humans[i]) < displayKey(humans[j]) })
	sort.Slice(mine, func(i, j int) bool { return mine[i].Username < mine[j].Username })
	return append(humans, mine...), nil
}

// displayKey is a case-insensitive sort key: display name when set, else handle.
func displayKey(u store.User) string {
	if u.Display != "" {
		return strings.ToLower(u.Display)
	}
	return strings.ToLower(u.Username)
}

func (s *Service) UpdateDescription(ctx context.Context, id, description string) error {
	if len([]rune(description)) > MaxDescriptionLen {
		return ErrInvalidDescription
	}
	item, err := s.store.ItemByID(ctx, id)
	if err != nil {
		return err
	}
	if item.Description == description {
		return nil
	}
	if err := s.store.UpdateItemDescription(ctx, id, description); err != nil {
		return err
	}
	s.recordEvent(ctx, item, store.EventItemDescribed, nil)
	return nil
}

// SetAssignee assigns the item to a user, or clears it when assigneeID is "".
func (s *Service) SetAssignee(ctx context.Context, id, assigneeID string) error {
	item, err := s.store.ItemByID(ctx, id)
	if err != nil {
		return err
	}
	if assigneeID != "" {
		if _, err := s.store.UserByID(ctx, assigneeID); err != nil {
			return err
		}
	}
	if item.AssigneeID == assigneeID {
		return nil
	}
	from := s.userName(ctx, item.AssigneeID)
	to := s.userName(ctx, assigneeID)
	if err := s.store.SetItemAssignee(ctx, id, assigneeID); err != nil {
		return err
	}
	s.recordEvent(ctx, item, store.EventItemAssigned, map[string]string{"from": from, "to": to})
	s.autoSubscribeAssignee(ctx, assigneeID, id) // assignee (and an agent's owner) watch the item
	return nil
}

// SetStatus changes an item's status. A top-level item is repositioned to the
// end of the target lane; a subtask (which isn't on the board) just takes the
// new status, keeping its order within its parent.
func (s *Service) SetStatus(ctx context.Context, id, statusID string) error {
	return s.setStatus(ctx, id, statusID, nil)
}

// setStatus is SetStatus with extra key/values merged into the status-change
// event's data (the confirmed-facts note on a checklist gate).
func (s *Service) setStatus(ctx context.Context, id, statusID string, extra map[string]string) error {
	item, err := s.store.ItemByID(ctx, id)
	if err != nil {
		return err
	}
	if err := s.requireStatusInWorkspace(ctx, statusID, item.WorkspaceID); err != nil {
		return err
	}
	if item.ParentID != "" {
		if item.StatusID == statusID {
			return nil
		}
		from, _ := s.store.StatusByID(ctx, item.StatusID)
		to, _ := s.store.StatusByID(ctx, statusID)
		if err := s.store.SetItemStatus(ctx, id, statusID); err != nil {
			return err
		}
		item.StatusID = statusID
		s.recordEvent(ctx, item, store.EventItemStatusChange,
			mergeData(s.statusChangeData(ctx, from, to), extra))
		return nil
	}
	return s.moveItem(ctx, id, statusID, endOfLane, extra)
}

// mergeData returns base with extra's non-empty entries merged in (extra wins).
func mergeData(base, extra map[string]string) map[string]string {
	for k, v := range extra {
		if v != "" {
			base[k] = v
		}
	}
	return base
}

// Archive hides an item and its whole subtree, then re-densifies the container
// (lane or parent) it left so positions stay contiguous.
func (s *Service) Archive(ctx context.Context, id string) error {
	item, err := s.store.ItemByID(ctx, id)
	if err != nil {
		return err
	}
	if err := s.archiveSubtree(ctx, id); err != nil {
		return err
	}
	if item.ParentID == "" {
		if err := s.densifyLane(ctx, item.StatusID); err != nil {
			return err
		}
	} else if err := s.densifyChildren(ctx, item.ParentID); err != nil {
		return err
	}
	s.recordEvent(ctx, item, store.EventItemArchived, nil)
	return nil
}

func (s *Service) archiveSubtree(ctx context.Context, id string) error {
	if err := s.store.ArchiveItem(ctx, id); err != nil {
		return err
	}
	kids, err := s.store.ChildrenByParent(ctx, id, false)
	if err != nil {
		return err
	}
	for _, k := range kids {
		if err := s.archiveSubtree(ctx, k.ID); err != nil {
			return err
		}
	}
	return nil
}

// Unarchive restores an item and its subtree, putting the root back at the end
// of its container.
func (s *Service) Unarchive(ctx context.Context, id string) error {
	item, err := s.store.ItemByID(ctx, id)
	if err != nil {
		return err
	}
	if err := s.unarchiveSubtree(ctx, id); err != nil {
		return err
	}
	if item.ParentID == "" {
		if err := s.appendToLaneEnd(ctx, item.StatusID, id); err != nil {
			return err
		}
	} else if err := s.appendToParentEnd(ctx, item.ParentID, id); err != nil {
		return err
	}
	s.recordEvent(ctx, item, store.EventItemUnarchived, nil)
	return nil
}

func (s *Service) unarchiveSubtree(ctx context.Context, id string) error {
	if err := s.store.UnarchiveItem(ctx, id); err != nil {
		return err
	}
	kids, err := s.store.ChildrenByParent(ctx, id, true) // all — they were cascade-archived
	if err != nil {
		return err
	}
	for _, k := range kids {
		if err := s.unarchiveSubtree(ctx, k.ID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) ArchivedItems(ctx context.Context, workspaceID string) ([]store.Item, error) {
	return s.store.ArchivedItemsByWorkspace(ctx, workspaceID)
}

func (s *Service) SubtaskCounts(ctx context.Context, workspaceID, doneStatusID string) (map[string]store.SubtaskCount, error) {
	return s.store.SubtaskCountsByWorkspace(ctx, workspaceID, doneStatusID)
}

// densifyLane renumbers a lane's active top-level items to 0..n-1.
func (s *Service) densifyLane(ctx context.Context, statusID string) error {
	active, err := s.store.ItemsByStatus(ctx, statusID)
	if err != nil {
		return err
	}
	return s.store.ReorderItems(ctx, statusID, idsOf(active))
}

// densifyChildren renumbers a parent's active children to 0..n-1.
func (s *Service) densifyChildren(ctx context.Context, parentID string) error {
	active, err := s.store.ChildrenByParent(ctx, parentID, false)
	if err != nil {
		return err
	}
	return s.store.SetItemPositions(ctx, idsOf(active))
}

func (s *Service) appendToLaneEnd(ctx context.Context, statusID, id string) error {
	active, err := s.store.ItemsByStatus(ctx, statusID)
	if err != nil {
		return err
	}
	return s.store.ReorderItems(ctx, statusID, appendLast(idsOf(active), id))
}

func (s *Service) appendToParentEnd(ctx context.Context, parentID, id string) error {
	active, err := s.store.ChildrenByParent(ctx, parentID, false)
	if err != nil {
		return err
	}
	return s.store.SetItemPositions(ctx, appendLast(idsOf(active), id))
}

func idsOf(items []store.Item) []string {
	ids := make([]string, len(items))
	for i, it := range items {
		ids[i] = it.ID
	}
	return ids
}

// appendLast returns ids with target removed (if present) then appended at the end.
func appendLast(ids []string, target string) []string {
	out := make([]string, 0, len(ids)+1)
	for _, id := range ids {
		if id != target {
			out = append(out, id)
		}
	}
	return append(out, target)
}

// --- subtasks ---

func (s *Service) CreateSubtask(ctx context.Context, parentID, title string) (store.Item, error) {
	return s.CreateSubtaskAs(ctx, parentID, title, "")
}

// CreateSubtaskAs is CreateSubtask attributing the new subtask to createdBy.
func (s *Service) CreateSubtaskAs(ctx context.Context, parentID, title, createdBy string) (store.Item, error) {
	title, err := cleanTitle(title)
	if err != nil {
		return store.Item{}, err
	}
	parent, err := s.store.ItemByID(ctx, parentID)
	if err != nil {
		return store.Item{}, err
	}
	statuses, err := s.store.StatusesByWorkspace(ctx, parent.WorkspaceID)
	if err != nil {
		return store.Item{}, err
	}
	if len(statuses) == 0 {
		return store.Item{}, ErrNoStatus
	}
	siblings, err := s.store.ChildrenByParent(ctx, parentID, false)
	if err != nil {
		return store.Item{}, err
	}
	it, err := s.store.CreateItem(ctx, store.Item{
		WorkspaceID: parent.WorkspaceID,
		StatusID:    statuses[0].ID, // a fresh subtask starts in the first lane
		ParentID:    parentID,
		Title:       title,
		Position:    len(siblings),
		CreatedBy:   createdBy,
	})
	if err != nil {
		return store.Item{}, err
	}
	// A new subtask inherits its parent's project (its area), so work filed under
	// a project keeps its children there by default. Overridable afterwards.
	if parent.ProjectID != "" {
		if err := s.store.SetItemProject(ctx, it.ID, parent.ProjectID); err != nil {
			return store.Item{}, err
		}
		it.ProjectID = parent.ProjectID
	}
	s.recordEvent(ctx, it, store.EventItemCreated, map[string]string{"status": statuses[0].Name})
	s.autoSubscribe(ctx, createdBy, store.SubjectItem, it.ID) // author watches what they create
	return it, nil
}

func (s *Service) Children(ctx context.Context, parentID string) ([]store.Item, error) {
	return s.store.ChildrenByParent(ctx, parentID, false)
}

func (s *Service) SetMilestone(ctx context.Context, id string, isMilestone bool) error {
	item, err := s.store.ItemByID(ctx, id)
	if err != nil {
		return err
	}
	if item.IsMilestone == isMilestone {
		return nil
	}
	// Capture the current milestone order before the flag flips, so a newly
	// promoted milestone lands at the end of the columns rather than tying at
	// the default ms_position 0.
	var existing []string
	if isMilestone {
		if existing, err = s.orderedMilestoneIDs(ctx, item.WorkspaceID); err != nil {
			return err
		}
	}
	if err := s.store.SetItemMilestone(ctx, id, isMilestone); err != nil {
		return err
	}
	if isMilestone {
		if err := s.store.ReorderMilestones(ctx, item.WorkspaceID, append(existing, id)); err != nil {
			return err
		}
	}
	item.IsMilestone = isMilestone
	s.recordEvent(ctx, item, store.EventItemMilestone, map[string]string{"on": boolStr(isMilestone)})
	return nil
}

// ReorderMilestones sets the column order of a workspace's milestones to the
// given id sequence (ids that aren't milestones in the workspace are ignored).
func (s *Service) ReorderMilestones(ctx context.Context, workspaceID string, orderedIDs []string) error {
	return s.store.ReorderMilestones(ctx, workspaceID, orderedIDs)
}

// orderedMilestoneIDs returns the workspace's root milestones in column order:
// by ms_position, with creation time as a stable tiebreak for any that still
// share a position (e.g. before the first reorder).
func (s *Service) orderedMilestoneIDs(ctx context.Context, workspaceID string) ([]string, error) {
	roots, err := s.store.ItemsByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	var ms []store.Item
	for _, it := range roots {
		if it.IsMilestone {
			ms = append(ms, it)
		}
	}
	sort.Slice(ms, func(i, j int) bool {
		if ms[i].MSPosition != ms[j].MSPosition {
			return ms[i].MSPosition < ms[j].MSPosition
		}
		return ms[i].CreatedAt.Before(ms[j].CreatedAt)
	})
	ids := make([]string, len(ms))
	for i, it := range ms {
		ids[i] = it.ID
	}
	return ids, nil
}

// CreateRootItem makes a top-level item; if statusID is "" it lands in the
// first lane (used by the Backlog column, which has no status of its own).
func (s *Service) CreateRootItem(ctx context.Context, workspaceID, statusID, title string) (store.Item, error) {
	return s.CreateRootItemAs(ctx, workspaceID, statusID, title, "")
}

// CreateRootItemAs is CreateRootItem attributing the new item to createdBy.
func (s *Service) CreateRootItemAs(ctx context.Context, workspaceID, statusID, title, createdBy string) (store.Item, error) {
	if statusID == "" {
		statuses, err := s.store.StatusesByWorkspace(ctx, workspaceID)
		if err != nil {
			return store.Item{}, err
		}
		if len(statuses) == 0 {
			return store.Item{}, ErrNoStatus
		}
		statusID = statuses[0].ID
	}
	return s.createItem(ctx, workspaceID, statusID, title, createdBy)
}

func (s *Service) ReorderSubtasks(ctx context.Context, parentID string, orderedIDs []string) error {
	return s.store.SetItemPositions(ctx, orderedIDs)
}

// Reparent moves an item under newParentID — "" promotes it back to the board
// (top-level), an item id demotes it under that item. Parenting under itself or
// one of its own descendants would form a cycle and is refused. The item keeps
// its status and lands at the end of its new container.
func (s *Service) Reparent(ctx context.Context, itemID, newParentID string) error {
	item, err := s.store.ItemByID(ctx, itemID)
	if err != nil {
		return err
	}
	if newParentID == itemID {
		return ErrCycle
	}
	if newParentID != "" {
		parent, err := s.store.ItemByID(ctx, newParentID)
		if err != nil {
			return err
		}
		if parent.WorkspaceID != item.WorkspaceID {
			return ErrStatusMismatch
		}
		desc, err := s.descendants(ctx, itemID)
		if err != nil {
			return err
		}
		if desc[newParentID] {
			return ErrCycle
		}
	}
	if item.ParentID == newParentID {
		return nil
	}
	if err := s.store.SetItemParent(ctx, itemID, newParentID); err != nil {
		return err
	}
	if item.ParentID == "" {
		if err := s.densifyLane(ctx, item.StatusID); err != nil {
			return err
		}
	} else if err := s.densifyChildren(ctx, item.ParentID); err != nil {
		return err
	}
	toParent := ""
	if newParentID == "" {
		if err := s.appendToLaneEnd(ctx, item.StatusID, itemID); err != nil {
			return err
		}
	} else {
		if err := s.appendToParentEnd(ctx, newParentID, itemID); err != nil {
			return err
		}
		if p, perr := s.store.ItemByID(ctx, newParentID); perr == nil {
			toParent = p.Title
		}
	}
	s.recordEvent(ctx, item, store.EventItemReparented, map[string]string{"to": toParent})
	return nil
}

// descendants returns the set of item ids in itemID's subtree (excluding itself).
func (s *Service) descendants(ctx context.Context, itemID string) (map[string]bool, error) {
	out := map[string]bool{}
	queue := []string{itemID}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		kids, err := s.store.ChildrenByParent(ctx, id, true)
		if err != nil {
			return nil, err
		}
		for _, k := range kids {
			if !out[k.ID] {
				out[k.ID] = true
				queue = append(queue, k.ID)
			}
		}
	}
	return out, nil
}

// CandidateParents lists the items an item may be reparented under: every active
// item except itself and its descendants.
func (s *Service) CandidateParents(ctx context.Context, workspaceID, itemID string) ([]store.Item, error) {
	all, err := s.store.AllItemsByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	desc, err := s.descendants(ctx, itemID)
	if err != nil {
		return nil, err
	}
	out := make([]store.Item, 0, len(all))
	for _, it := range all {
		if it.ID == itemID || desc[it.ID] {
			continue
		}
		out = append(out, it)
	}
	return out, nil
}

// --- comments ---

// Comments returns an item's live comments (soft-deleted ones excluded),
// oldest-first — the default any consumer wants. The web activity feed, which
// renders a tombstone for deleted comments, uses CommentsWithDeleted instead.
func (s *Service) Comments(ctx context.Context, itemID string) ([]store.Comment, error) {
	all, err := s.store.CommentsByItem(ctx, itemID)
	if err != nil {
		return nil, err
	}
	out := all[:0]
	for _, c := range all {
		if c.DeletedAt == nil {
			out = append(out, c)
		}
	}
	return out, nil
}

// CommentsWithDeleted returns every comment including soft-deleted tombstones.
func (s *Service) CommentsWithDeleted(ctx context.Context, itemID string) ([]store.Comment, error) {
	return s.store.CommentsByItem(ctx, itemID)
}

// EditComment replaces a comment's body. Only the author may edit, and only a
// live (non-deleted) comment. The body is validated like a new comment. Any
// @mention the edit newly introduces is notified (existing mentions are not
// re-pinged); the notifications are returned so the caller can bump bells.
func (s *Service) EditComment(ctx context.Context, commentID, actorID, body string) (store.Comment, []store.Notification, error) {
	body = strings.TrimSpace(body)
	if body == "" || len([]rune(body)) > MaxCommentLen {
		return store.Comment{}, nil, ErrInvalidComment
	}
	c, err := s.store.CommentByID(ctx, commentID)
	if err != nil {
		return store.Comment{}, nil, err
	}
	if c.DeletedAt != nil {
		return store.Comment{}, nil, store.ErrCommentNotFound
	}
	if c.AuthorID == "" || c.AuthorID != actorID {
		return store.Comment{}, nil, ErrCommentForbidden
	}
	updated, err := s.store.UpdateComment(ctx, commentID, body, s.now())
	if err != nil {
		return store.Comment{}, nil, err
	}
	var notified []store.Notification
	if added := newMentions(updated.Body, c.Body); len(added) > 0 {
		if item, ierr := s.store.ItemByID(ctx, updated.ItemID); ierr == nil {
			notified = s.notifyHandles(ctx, item, updated, updated.Body, added)
		}
	}
	return updated, notified, nil
}

// DeleteComment soft-deletes a comment. Only the author may delete; deleting an
// already-deleted comment is a no-op (idempotent).
func (s *Service) DeleteComment(ctx context.Context, commentID, actorID string) (store.Comment, error) {
	c, err := s.store.CommentByID(ctx, commentID)
	if err != nil {
		return store.Comment{}, err
	}
	if c.DeletedAt != nil {
		return c, nil
	}
	if c.AuthorID == "" || c.AuthorID != actorID {
		return store.Comment{}, ErrCommentForbidden
	}
	return s.store.SoftDeleteComment(ctx, commentID, s.now())
}

// AddComment appends a comment and, for each resolvable @mention in it, files a
// notification. It returns the comment plus the notifications it created, so a
// caller (the web layer) can push a live bell update to each recipient.
func (s *Service) AddComment(ctx context.Context, itemID, authorID, body string) (store.Comment, []store.Notification, error) {
	body = strings.TrimSpace(body)
	if body == "" || len([]rune(body)) > MaxCommentLen {
		return store.Comment{}, nil, ErrInvalidComment
	}
	c, err := s.store.CreateComment(ctx, store.Comment{
		ItemID:   itemID,
		AuthorID: authorID,
		Body:     body,
	})
	if err != nil {
		return store.Comment{}, nil, err
	}
	var notified []store.Notification
	if item, ierr := s.store.ItemByID(ctx, itemID); ierr == nil {
		// Mentions first: they're the more specific delivery, so anyone @mentioned
		// is excluded from the comment's generic activity fanout (no double-ping).
		notified = s.notifyMentions(ctx, item, c, body)
		s.recordEventExcluding(ctx, item, store.EventCommentAdded,
			map[string]string{"excerpt": excerpt(body, 100)}, recipientSet(notified))
		s.autoSubscribe(ctx, authorID, store.SubjectItem, itemID) // commenting follows the thread
	}
	return c, notified, nil
}

// --- documents ---
//
// Documents are titled markdown artifacts on an item (many per item), edited in
// place. Unlike comments they're not author-locked: anyone who can see the item
// can add, edit, or remove its documents — they're shared task artifacts, like
// the description, not personal posts. AuthorID records the creator for display.

// Documents returns an item's documents, oldest-first.
func (s *Service) Documents(ctx context.Context, itemID string) ([]store.Document, error) {
	return s.store.DocumentsByItem(ctx, itemID)
}

// Document returns a single document by id.
func (s *Service) Document(ctx context.Context, id string) (store.Document, error) {
	return s.store.DocumentByID(ctx, id)
}

// AddDocument attaches a new document to an item. The title is required (trimmed,
// bounded); the body is optional markdown, bounded at MaxDocumentLen.
func (s *Service) AddDocument(ctx context.Context, itemID, authorID, title, body string) (store.Document, error) {
	title = strings.TrimSpace(title)
	if title == "" || len([]rune(title)) > MaxDocumentTitleLen || len([]rune(body)) > MaxDocumentLen {
		return store.Document{}, ErrInvalidDocument
	}
	item, err := s.store.ItemByID(ctx, itemID)
	if err != nil {
		return store.Document{}, err
	}
	d, err := s.store.CreateDocument(ctx, store.Document{
		ItemID:   itemID,
		AuthorID: authorID,
		Title:    title,
		Body:     body,
	})
	if err != nil {
		return store.Document{}, err
	}
	s.recordEvent(ctx, item, store.EventDocumentAdded, map[string]string{"title": title})
	s.autoSubscribe(ctx, authorID, store.SubjectItem, itemID) // authoring follows the item
	return d, nil
}

// EditDocument replaces a document's title and body in place, stamping updatedAt.
func (s *Service) EditDocument(ctx context.Context, docID, title, body string) (store.Document, error) {
	title = strings.TrimSpace(title)
	if title == "" || len([]rune(title)) > MaxDocumentTitleLen || len([]rune(body)) > MaxDocumentLen {
		return store.Document{}, ErrInvalidDocument
	}
	existing, err := s.store.DocumentByID(ctx, docID)
	if err != nil {
		return store.Document{}, err
	}
	d, err := s.store.UpdateDocument(ctx, docID, title, body, s.now())
	if err != nil {
		return store.Document{}, err
	}
	if item, ierr := s.store.ItemByID(ctx, existing.ItemID); ierr == nil {
		s.recordEvent(ctx, item, store.EventDocumentUpdated, map[string]string{"title": title})
	}
	return d, nil
}

// RemoveDocument deletes a document and returns the removed row (for the
// activity event and any live fanout).
func (s *Service) RemoveDocument(ctx context.Context, docID string) (store.Document, error) {
	d, err := s.store.DocumentByID(ctx, docID)
	if err != nil {
		return store.Document{}, err
	}
	if err := s.store.DeleteDocument(ctx, docID); err != nil {
		return store.Document{}, err
	}
	if item, ierr := s.store.ItemByID(ctx, d.ItemID); ierr == nil {
		s.recordEvent(ctx, item, store.EventDocumentRemoved, map[string]string{"title": d.Title})
	}
	return d, nil
}

// mentionRe matches an @handle. The handle starts alphanumeric and may contain
// dot, underscore, hyphen and slash, so it spans both human usernames and the
// owner/name form of an agent (e.g. @jack/deploy-bot). Match is
// case-insensitive; resolution lowercases since stored usernames are lowercase.
var mentionRe = regexp.MustCompile(`@([A-Za-z0-9][A-Za-z0-9._/-]*)`)

// parseMentions returns the distinct @handles in body, lowercased and with
// trailing punctuation trimmed (so "@jack." yields "jack"), in first-seen
// order. It is purely lexical — resolving a handle to a principal is the
// caller's job.
func parseMentions(body string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range mentionRe.FindAllStringSubmatch(body, -1) {
		h := strings.ToLower(strings.TrimRight(m[1], "._-/"))
		if h == "" || seen[h] {
			continue
		}
		seen[h] = true
		out = append(out, h)
	}
	return out
}

// notifyMentions delivers to every distinct, resolvable @handle in a
// freshly-added comment. An edit instead calls notifyHandles with only the
// handles it introduced (see newMentions), so re-saving doesn't re-ping people.
func (s *Service) notifyMentions(ctx context.Context, item store.Item, c store.Comment, body string) []store.Notification {
	return s.notifyHandles(ctx, item, c, body, parseMentions(body))
}

// newMentions returns the handles present in newBody but not oldBody (set
// difference, first-seen order) — the mentions an edit added.
func newMentions(newBody, oldBody string) []string {
	had := map[string]bool{}
	for _, h := range parseMentions(oldBody) {
		had[h] = true
	}
	var out []string
	for _, h := range parseMentions(newBody) {
		if !had[h] {
			out = append(out, h)
		}
	}
	return out
}

// notifyHandles writes a mention notification for each handle in handles,
// attributing it to the acting principal and excerpting body. Self-mentions are
// intentionally allowed — @-ing yourself bookmarks a thread. Best-effort like
// the activity log: a failed write is logged, never surfaced — a missed
// notification must not fail the comment that triggered it. The recipient's
// inbox is the single source both the bell and the MCP poll read from, so this
// is the only place mentions become deliveries.
func (s *Service) notifyHandles(ctx context.Context, item store.Item, c store.Comment, body string, handles []string) []store.Notification {
	if len(handles) == 0 {
		return nil
	}
	actorID := c.AuthorID
	actorName := s.userName(ctx, c.AuthorID)
	if p, ok := identity.FromContext(ctx); ok && p != nil {
		actorID = p.ID
		actorName = principalName(p)
	}
	slug := ""
	if ws, err := s.store.WorkspaceByID(ctx, item.WorkspaceID); err == nil {
		slug = ws.Slug
	}
	ex := excerpt(body, 140)
	var notified []store.Notification
	for _, h := range handles {
		u, err := s.store.UserByUsername(ctx, h)
		if err != nil || u.ID == "" {
			continue // unknown @handle
		}
		n, err := s.store.CreateNotification(ctx, store.Notification{
			RecipientID:   u.ID,
			Kind:          store.NotificationMention,
			WorkspaceID:   item.WorkspaceID,
			WorkspaceSlug: slug,
			ItemID:        item.ID,
			ItemTitle:     item.Title,
			ActorID:       actorID,
			ActorName:     actorName,
			CommentID:     c.ID,
			Excerpt:       ex,
		})
		if err != nil {
			slog.Error("create mention notification", "recipient", u.ID, "item", item.ID, "err", err)
			continue
		}
		notified = append(notified, n)
		s.notify(ctx, u.ID, n) // best-effort push + live bell; must not block
	}
	return notified
}

// --- notifications (inbox reads, used by the bell) ---

// Notifications returns a recipient's most recent inbox entries, newest first.
func (s *Service) Notifications(ctx context.Context, recipientID string, limit int) ([]store.Notification, error) {
	return s.store.NotificationsByRecipient(ctx, recipientID, limit)
}

// UnreadNotifications returns the recipient's unread inbox entries, newest
// first — the set an agent drains by polling and marking each read.
func (s *Service) UnreadNotifications(ctx context.Context, recipientID string, limit int) ([]store.Notification, error) {
	return s.store.UnreadNotificationsByRecipient(ctx, recipientID, limit)
}

// UnreadCount is the recipient's unread notification count (the bell badge).
func (s *Service) UnreadCount(ctx context.Context, recipientID string) (int, error) {
	return s.store.UnreadNotificationCount(ctx, recipientID)
}

// MarkNotificationRead marks one of the recipient's notifications read; scoping
// to recipientID stops one principal clearing another's. Idempotent.
func (s *Service) MarkNotificationRead(ctx context.Context, id, recipientID string) error {
	return s.store.MarkNotificationRead(ctx, id, recipientID)
}

// MarkAllNotificationsRead clears the recipient's whole unread set.
func (s *Service) MarkAllNotificationsRead(ctx context.Context, recipientID string) error {
	return s.store.MarkAllNotificationsRead(ctx, recipientID)
}

// --- activity ---

// ItemHistory returns the most recent activity-log entries for one item,
// newest first.
func (s *Service) ItemHistory(ctx context.Context, itemID string, limit int) ([]store.Event, error) {
	return s.store.EventsByItem(ctx, itemID, limit)
}

// WorkspaceActivity returns the most recent activity-log entries across a whole
// workspace, newest first.
func (s *Service) WorkspaceActivity(ctx context.Context, workspaceID string, limit int) ([]store.Event, error) {
	return s.store.EventsByWorkspace(ctx, workspaceID, limit)
}

// BoardActivity returns the most recent activity-log entries for one board,
// newest first.
func (s *Service) BoardActivity(ctx context.Context, boardID string, limit int) ([]store.Event, error) {
	return s.store.EventsByBoard(ctx, boardID, limit)
}

// SeedDefaults gives a new workspace its two starter boards: "Tasks" (To do /
// Doing / Done) and "Backlog" (a single Backlog lane). The first lane of each
// board is its entry lane. This mirrors migration 0020 for workspaces created
// after boards existed.
func (s *Service) SeedDefaults(ctx context.Context, workspaceID string) error {
	if err := s.seedBoard(ctx, workspaceID, "Tasks", "tasks", 0, DefaultStatuses); err != nil {
		return err
	}
	return s.seedBoard(ctx, workspaceID, "Backlog", "backlog", 1, DefaultBacklogStatuses)
}

// seedBoard creates one board and its lanes, flagging the first lane as the
// board's entry lane.
func (s *Service) seedBoard(ctx context.Context, workspaceID, name, slug string, position int, lanes []string) error {
	b, err := s.store.CreateBoard(ctx, store.Board{
		WorkspaceID: workspaceID,
		Name:        name,
		Slug:        slug,
		Position:    position,
	})
	if err != nil {
		return err
	}
	for i, lane := range lanes {
		if _, err := s.store.CreateStatus(ctx, store.Status{
			WorkspaceID: workspaceID,
			BoardID:     b.ID,
			Name:        lane,
			Position:    i,
			IsEntry:     i == 0,
		}); err != nil {
			return err
		}
	}
	return s.seedBoardViews(ctx, workspaceID, b.ID)
}

// requireStatusInWorkspace confirms a status exists and belongs to workspaceID.
func (s *Service) requireStatusInWorkspace(ctx context.Context, statusID, workspaceID string) error {
	st, err := s.store.StatusByID(ctx, statusID)
	if err != nil {
		return err
	}
	if st.WorkspaceID != workspaceID {
		return ErrStatusMismatch
	}
	return nil
}

func cleanName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > MaxStatusNameLen {
		return "", ErrInvalidName
	}
	return name, nil
}

func cleanTitle(title string) (string, error) {
	title = strings.TrimSpace(title)
	if title == "" || len([]rune(title)) > MaxItemTitleLen {
		return "", ErrInvalidTitle
	}
	return title, nil
}
