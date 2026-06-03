package web

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/peios/acta/internal/board"
	"github.com/peios/acta/internal/store"
)

// mcpHandler builds the Model Context Protocol endpoint. It is a sibling of the
// REST API — an agent-shaped presentation of the same board service — sharing
// its Bearer (PAT) auth and the same name<->id translation, so the two surfaces
// can't drift. Every write is attributed to the calling principal, so pointing
// an MCP client at an *agent* PAT (jack/claude) records that agent as the author
// of the items it creates and the comments it leaves.
//
// The transport is Streamable HTTP in stateless, JSON-response mode: each
// Bearer-authed POST is self-contained (no server-side session, no Mcp-Session-Id
// to track) and responses are plain application/json rather than SSE. That suits
// our purely request/response tools and survives the request-logging middleware,
// which does not support streaming.
func (h *handlers) mcpHandler() http.Handler {
	// The server is rebuilt per request (cheap, and the transport is stateless
	// so nothing is cached between requests). That lets the resource and prompt
	// sets reflect the current database — a guide edit or a new custom prompt is
	// live on the very next request, with no restart. The request context here
	// already carries the authenticated principal (the /mcp route is wrapped in
	// requireToken), so the registration reads use it directly.
	return mcp.NewStreamableHTTPHandler(
		func(r *http.Request) *mcp.Server {
			srv := mcp.NewServer(&mcp.Implementation{Name: "acta", Version: "v1"}, nil)
			h.registerMCPTools(srv)
			h.registerMCPResources(r.Context(), srv)
			h.registerMCPPrompts(r.Context(), srv)
			return srv
		},
		&mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true},
	)
}

func (h *handlers) registerMCPTools(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "whoami",
		Description: "Return the principal the current token authenticates as. is_agent is true for an agent (username owner/agentname), with owner naming the human it acts for.",
	}, h.mcpWhoami)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_workspaces",
		Description: "List the workspaces (boards). Each has a slug (used to address it in other tools) and a display name.",
	}, h.mcpListWorkspaces)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_statuses",
		Description: "List a workspace's status lanes, in board order. Statuses are addressed by name (in create_item, set_item_status, and the list_items status filter), and the names differ per board — call this to learn the exact lanes instead of guessing. The first lane is the default for new items.",
	}, h.mcpListStatuses)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_items",
		Description: "List items on a workspace board. Returns top-level items by default; pass parent to list the direct subtasks of an item instead. Optional filters narrow by status (lane name), assignee (username, or \"me\"), or mine. Statuses and principals are named, not id-addressed; items are addressed by id.",
	}, h.mcpListItems)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_item",
		Description: "Fetch one item by id with its full context: status, assignee, author, parent, its direct subtasks, and all comments oldest-first.",
	}, h.mcpGetItem)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "create_item",
		Description: "Create an item on a workspace board. Provide a status lane by name (defaults to the first lane) or a parent item id to create it as a subtask. The item is attributed to the calling principal.",
	}, h.mcpCreateItem)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "set_item_status",
		Description: "Move an item to a different status lane, named (e.g. \"Doing\", \"Done\").",
	}, h.mcpSetItemStatus)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "set_item_assignee",
		Description: "Assign an item to a user by username (\"me\" assigns it to the caller). Omit assignee to clear the assignment.",
	}, h.mcpSetItemAssignee)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "set_item_description",
		Description: "Set an item's description (its long-form body), replacing any existing one. Pass an empty description to clear it.",
	}, h.mcpSetItemDescription)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "set_item_milestone",
		Description: "Flag or unflag an item as a milestone.",
	}, h.mcpSetItemMilestone)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "set_item_parent",
		Description: "Reparent an item: pass a parent item id to nest it under that item (same workspace), or omit parent to promote it to a top-level board item. An item can't be parented under itself or one of its own descendants.",
	}, h.mcpSetItemParent)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "add_comment",
		Description: "Append a comment to an item, authored by the calling principal. Comments are how agents record progress and coordinate.",
	}, h.mcpAddComment)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "archive_item",
		Description: "Archive an item (and its subtree), hiding it from the board. Reversible with unarchive_item.",
	}, h.mcpArchiveItem)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "unarchive_item",
		Description: "Restore a previously archived item (and its subtree) to the board.",
	}, h.mcpUnarchiveItem)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_activity",
		Description: "Read the activity log, newest first: who changed what and when (creations, status moves, assignments, comments, archives, …). Pass item to get one item's history, or omit it for the whole workspace feed. Use this to answer \"what changed since yesterday\" for a standup instead of diffing the board. Each entry has a human-readable summary plus the raw verb and data for precise parsing.",
	}, h.mcpListActivity)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_notifications",
		Description: "Poll your notification inbox, newest first. Returns unread notifications by default — the set to drain — so an idle agent can long-poll this to learn when a human @mentions it in a comment. Set include_read to also list ones already marked read. Each entry names the actor, the item it points at (id, workspace slug, permalink url), and an excerpt of the comment. Act on one, then call mark_notification_read with its id.",
	}, h.mcpListNotifications)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "mark_notification_read",
		Description: "Mark one of your notifications read by id (ids come from list_notifications), clearing it from the unread inbox. Idempotent: an already-read, unknown, or someone else's id is a no-op. Returns your remaining unread count.",
	}, h.mcpMarkNotificationRead)
}

// --- tool input/output types ---

type emptyInput struct{}

type principalView struct {
	Username string `json:"username"`
	Display  string `json:"display,omitempty"`
	IsAgent  bool   `json:"is_agent"`
	Owner    string `json:"owner,omitempty"` // owning human, when is_agent
}

// mcpItem is the agent-facing item shape. It is deliberately separate from the
// REST itemAPI, which is self-referential (Subtasks []itemAPI) — the MCP output
// schema is generated and validated, and the generator rejects recursive types.
// Name<->id translation is still shared (toMCPItem rides the same name maps).
type mcpItem struct {
	ID            string `json:"id"`
	Ref           string `json:"ref,omitempty"` // human id, e.g. "ACTA-12" (also accepted wherever an id is)
	Title         string `json:"title"`
	Status        string `json:"status"`
	Assignee      string `json:"assignee,omitempty"`
	Milestone     bool   `json:"milestone,omitempty"`
	Archived      bool   `json:"archived,omitempty"`
	CreatedBy     string `json:"created_by,omitempty"`
	CreatedAt     string `json:"created_at,omitempty"`
	ParentID      string `json:"parent_id,omitempty"`
	URL           string `json:"url,omitempty"` // permalink to open the item on the board
	SubtasksDone  int    `json:"subtasks_done,omitempty"`
	SubtasksTotal int    `json:"subtasks_total,omitempty"`
}

// mcpItemDetail is the deep read: the item plus its description, one level of
// subtasks, and its comments. Subtasks are leaf mcpItems, so it isn't recursive.
type mcpItemDetail struct {
	mcpItem
	Description string       `json:"description,omitempty"`
	Subtasks    []mcpItem    `json:"subtasks"`
	Comments    []commentAPI `json:"comments"`
}

// commentAPI is a comment as the MCP surface presents it: author by username,
// body, and an RFC3339 timestamp.
type commentAPI struct {
	Author string `json:"author,omitempty"`
	Body   string `json:"body"`
	At     string `json:"at"`
}

type workspaceListOutput struct {
	Workspaces []workspaceAPI `json:"workspaces"`
}

type statusListOutput struct {
	Statuses []statusAPI `json:"statuses"`
}

// statusAPI is a board lane as the MCP surface presents it: the name agents
// address it by, and its zero-based board position (position 0 is the first
// lane, the default for new items). Colour is omitted as UI-only.
type statusAPI struct {
	Name     string `json:"name"`
	Position int    `json:"position"`
}

type itemListOutput struct {
	Items []mcpItem `json:"items"`
}

func toMCPItem(it store.Item, statusName, userName map[string]string, prefix string) mcpItem {
	return mcpItem{
		ID:        it.ID,
		Ref:       refID(prefix, it.RefNum),
		Title:     it.Title,
		Status:    statusName[it.StatusID],
		Assignee:  userName[it.AssigneeID],
		Milestone: it.IsMilestone,
		Archived:  it.ArchivedAt != nil,
		CreatedBy: userName[it.CreatedBy],
		CreatedAt: it.CreatedAt.Format(time.RFC3339),
		ParentID:  it.ParentID,
	}
}

// itemURL builds a browser permalink that opens the item on its board, or ""
// when the public origin or slug is unknown (the field is then omitted).
func (h *handlers) itemURL(slug, id string) string {
	if h.publicURL == "" || slug == "" {
		return ""
	}
	return h.publicURL + "/" + slug + "?item=" + id
}

// slugFor resolves a workspace id to its slug for permalink building. Errors are
// swallowed — the permalink is a convenience, not load-bearing.
func (h *handlers) slugFor(ctx context.Context, workspaceID string) string {
	ws, err := h.workspaces.ByID(ctx, workspaceID)
	if err != nil {
		return ""
	}
	return ws.Slug
}

// prefixFor resolves a workspace id to its item-id prefix, for building human
// ids. Errors are swallowed — the prefix is presentational, not load-bearing.
func (h *handlers) prefixFor(ctx context.Context, workspaceID string) string {
	ws, err := h.workspaces.ByID(ctx, workspaceID)
	if err != nil {
		return ""
	}
	return ws.ItemPrefix
}

type listStatusesInput struct {
	Workspace string `json:"workspace" jsonschema:"slug of the workspace whose status lanes to list"`
}

type listItemsInput struct {
	Workspace string `json:"workspace" jsonschema:"slug of the workspace to list"`
	Status    string `json:"status,omitempty" jsonschema:"only items in this status lane, by name"`
	Assignee  string `json:"assignee,omitempty" jsonschema:"only items assigned to this username; use \"me\" for the caller"`
	Parent    string `json:"parent,omitempty" jsonschema:"list the direct subtasks of this item id instead of the board's top-level items"`
	Mine      bool   `json:"mine,omitempty" jsonschema:"only items assigned to the calling principal (shorthand for assignee=me)"`
}

type itemIDInput struct {
	ID string `json:"id" jsonschema:"the item id"`
}

type createItemInput struct {
	Workspace string `json:"workspace" jsonschema:"slug of the workspace to create the item in"`
	Title     string `json:"title" jsonschema:"the item title"`
	Status    string `json:"status,omitempty" jsonschema:"status lane by name; defaults to the first lane. Ignored when parent is set"`
	Parent    string `json:"parent,omitempty" jsonschema:"parent item id; when set, create this as a subtask of that item"`
}

type setItemStatusInput struct {
	ID     string `json:"id" jsonschema:"the item id"`
	Status string `json:"status" jsonschema:"target status lane, by name"`
}

type setItemAssigneeInput struct {
	ID       string `json:"id" jsonschema:"the item id"`
	Assignee string `json:"assignee,omitempty" jsonschema:"username to assign to; \"me\" for the caller; omit to clear the assignment"`
}

type addCommentInput struct {
	ID   string `json:"id" jsonschema:"the item id to comment on"`
	Body string `json:"body" jsonschema:"the comment text"`
}

type setItemDescriptionInput struct {
	ID          string `json:"id" jsonschema:"the item id"`
	Description string `json:"description" jsonschema:"the new description; empty clears it"`
}

type setItemMilestoneInput struct {
	ID        string `json:"id" jsonschema:"the item id"`
	Milestone bool   `json:"milestone" jsonschema:"true to flag as a milestone, false to unflag"`
}

type setItemParentInput struct {
	ID     string `json:"id" jsonschema:"the item id to reparent"`
	Parent string `json:"parent,omitempty" jsonschema:"new parent item id; omit to promote to a top-level board item"`
}

type listActivityInput struct {
	Workspace string `json:"workspace" jsonschema:"slug of the workspace whose activity to read"`
	Item      string `json:"item,omitempty" jsonschema:"restrict to a single item id's history; omit for the whole-workspace feed"`
	Limit     int    `json:"limit,omitempty" jsonschema:"max entries to return, newest first (default 50)"`
}

// mcpEvent is one activity-log entry for agents: a human-readable summary plus
// the raw verb and resolved data fields, so it can be read at a glance or parsed.
type mcpEvent struct {
	Actor     string            `json:"actor,omitempty"`
	Verb      string            `json:"verb"`
	Summary   string            `json:"summary"`
	ItemID    string            `json:"item_id,omitempty"`
	ItemTitle string            `json:"item_title,omitempty"`
	Data      map[string]string `json:"data,omitempty"`
	At        string            `json:"at"`
}

type activityOutput struct {
	Events []mcpEvent `json:"events"`
}

type listNotificationsInput struct {
	IncludeRead bool `json:"include_read,omitempty" jsonschema:"also include notifications already marked read; default false (unread only)"`
	Limit       int  `json:"limit,omitempty" jsonschema:"max notifications to return, newest first (default 50)"`
}

type markNotificationReadInput struct {
	ID string `json:"id" jsonschema:"the notification id to mark read (from list_notifications)"`
}

// mcpNotification is one inbox entry for agents: who triggered it, the item it
// points at (with a permalink), an excerpt, and whether it is still unread.
type mcpNotification struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Unread    bool   `json:"unread"`
	Actor     string `json:"actor,omitempty"`
	Workspace string `json:"workspace,omitempty"` // slug of the item's board
	ItemID    string `json:"item_id,omitempty"`
	ItemTitle string `json:"item_title,omitempty"`
	Excerpt   string `json:"excerpt,omitempty"`
	URL       string `json:"url,omitempty"` // permalink to open the item on the board
	At        string `json:"at"`
}

type notificationsOutput struct {
	Notifications []mcpNotification `json:"notifications"`
	Unread        int               `json:"unread"` // the caller's total unread count
}

// markReadOutput reports the caller's remaining unread count after a mark.
type markReadOutput struct {
	Unread int `json:"unread"`
}

// --- tool handlers ---

func (h *handlers) mcpWhoami(ctx context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, principalView, error) {
	p := principalFrom(ctx)
	if p == nil {
		return nil, principalView{}, errors.New("not authenticated")
	}
	v := principalView{Username: p.Username, Display: p.Display}
	if owner, _, ok := strings.Cut(p.Username, "/"); ok {
		v.IsAgent = true
		v.Owner = owner
	}
	return &mcp.CallToolResult{}, v, nil
}

func (h *handlers) mcpListWorkspaces(ctx context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, workspaceListOutput, error) {
	list, err := h.workspaces.List(ctx)
	if err != nil {
		return nil, workspaceListOutput{}, mcpErr(err)
	}
	out := workspaceListOutput{Workspaces: make([]workspaceAPI, len(list))}
	for i, ws := range list {
		out.Workspaces[i] = workspaceAPI{Slug: ws.Slug, Name: ws.Name}
	}
	return &mcp.CallToolResult{}, out, nil
}

func (h *handlers) mcpListStatuses(ctx context.Context, _ *mcp.CallToolRequest, in listStatusesInput) (*mcp.CallToolResult, statusListOutput, error) {
	ws, err := h.mcpWorkspace(ctx, in.Workspace)
	if err != nil {
		return nil, statusListOutput{}, err
	}
	list, err := h.board.Statuses(ctx, ws.ID)
	if err != nil {
		return nil, statusListOutput{}, mcpErr(err)
	}
	out := statusListOutput{Statuses: make([]statusAPI, len(list))}
	for i, s := range list {
		out.Statuses[i] = statusAPI{Name: s.Name, Position: s.Position}
	}
	return &mcp.CallToolResult{}, out, nil
}

func (h *handlers) mcpListItems(ctx context.Context, _ *mcp.CallToolRequest, in listItemsInput) (*mcp.CallToolResult, itemListOutput, error) {
	ws, err := h.mcpWorkspace(ctx, in.Workspace)
	if err != nil {
		return nil, itemListOutput{}, err
	}

	// Resolve filters up front so a bad name fails before we list.
	statusID := ""
	if s := strings.TrimSpace(in.Status); s != "" {
		statusID, err = h.statusIDByName(ctx, ws.ID, s)
		if err != nil {
			return nil, itemListOutput{}, mcpErr(err)
		}
	}
	assigneeID, filterAssignee, err := h.resolveAssignee(ctx, in.Assignee, in.Mine)
	if err != nil {
		return nil, itemListOutput{}, mcpErr(err)
	}

	// Base set: a parent's direct subtasks, or the board's top-level items.
	var items []store.Item
	if parent := strings.ToLower(strings.TrimSpace(in.Parent)); parent != "" {
		if _, err := h.mcpItem(ctx, parent, ws.ID); err != nil {
			return nil, itemListOutput{}, err
		}
		items, err = h.board.Children(ctx, parent)
	} else {
		items, err = h.board.Items(ctx, ws.ID)
	}
	if err != nil {
		return nil, itemListOutput{}, mcpErr(err)
	}

	statuses, err := h.board.Statuses(ctx, ws.ID)
	if err != nil {
		return nil, itemListOutput{}, mcpErr(err)
	}
	users, err := h.board.Users(ctx)
	if err != nil {
		return nil, itemListOutput{}, mcpErr(err)
	}
	statusName := make(map[string]string, len(statuses))
	for _, s := range statuses {
		statusName[s.ID] = s.Name
	}
	userName := make(map[string]string, len(users))
	for _, u := range users {
		userName[u.ID] = u.Username
	}
	doneStatusID := ""
	if len(statuses) > 0 {
		doneStatusID = statuses[len(statuses)-1].ID
	}
	counts, err := h.board.SubtaskCounts(ctx, ws.ID, doneStatusID)
	if err != nil {
		return nil, itemListOutput{}, mcpErr(err)
	}

	out := itemListOutput{Items: []mcpItem{}}
	for _, it := range items {
		if statusID != "" && it.StatusID != statusID {
			continue
		}
		if filterAssignee && it.AssigneeID != assigneeID {
			continue
		}
		v := toMCPItem(it, statusName, userName, ws.ItemPrefix)
		v.URL = h.itemURL(ws.Slug, it.ID)
		if c, ok := counts[it.ID]; ok && c.Total > 0 {
			v.SubtasksDone, v.SubtasksTotal = c.Done, c.Total
		}
		out.Items = append(out.Items, v)
	}
	return &mcp.CallToolResult{}, out, nil
}

func (h *handlers) mcpGetItem(ctx context.Context, _ *mcp.CallToolRequest, in itemIDInput) (*mcp.CallToolResult, mcpItemDetail, error) {
	item, err := h.mcpItem(ctx, in.ID, "")
	if err != nil {
		return nil, mcpItemDetail{}, err
	}
	statusName, userName, err := h.nameMaps(ctx, item.WorkspaceID)
	if err != nil {
		return nil, mcpItemDetail{}, mcpErr(err)
	}
	slug := h.slugFor(ctx, item.WorkspaceID)
	prefix := h.prefixFor(ctx, item.WorkspaceID)
	root := toMCPItem(item, statusName, userName, prefix)
	root.URL = h.itemURL(slug, item.ID)
	detail := mcpItemDetail{
		mcpItem:     root,
		Description: item.Description,
		Subtasks:    []mcpItem{},
		Comments:    []commentAPI{},
	}

	children, err := h.board.Children(ctx, item.ID)
	if err != nil {
		return nil, mcpItemDetail{}, mcpErr(err)
	}
	for _, c := range children {
		cv := toMCPItem(c, statusName, userName, prefix)
		cv.URL = h.itemURL(slug, c.ID)
		detail.Subtasks = append(detail.Subtasks, cv)
	}

	comments, err := h.board.Comments(ctx, item.ID)
	if err != nil {
		return nil, mcpItemDetail{}, mcpErr(err)
	}
	for _, c := range comments {
		detail.Comments = append(detail.Comments, commentAPI{
			Author: userName[c.AuthorID],
			Body:   c.Body,
			At:     c.CreatedAt.Format(time.RFC3339),
		})
	}
	return &mcp.CallToolResult{}, detail, nil
}

func (h *handlers) mcpCreateItem(ctx context.Context, _ *mcp.CallToolRequest, in createItemInput) (*mcp.CallToolResult, mcpItem, error) {
	ws, err := h.mcpWorkspace(ctx, in.Workspace)
	if err != nil {
		return nil, mcpItem{}, err
	}
	p := principalFrom(ctx)

	var it store.Item
	if parent := strings.ToLower(strings.TrimSpace(in.Parent)); parent != "" {
		if _, err := h.mcpItem(ctx, parent, ws.ID); err != nil {
			return nil, mcpItem{}, err
		}
		it, err = h.board.CreateSubtaskAs(ctx, parent, in.Title, p.ID)
	} else {
		statusID := ""
		if s := strings.TrimSpace(in.Status); s != "" {
			statusID, err = h.statusIDByName(ctx, ws.ID, s)
			if err != nil {
				return nil, mcpItem{}, mcpErr(err)
			}
		}
		it, err = h.board.CreateRootItemAs(ctx, ws.ID, statusID, in.Title, p.ID)
	}
	if err != nil {
		return nil, mcpItem{}, mcpErr(err)
	}
	if it.ParentID != "" {
		h.publishSubtaskAdd("", it.WorkspaceID, it)
	} else {
		h.publishItemUpsert(ctx, "", it.WorkspaceID, it)
	}
	return h.mcpItemResult(ctx, it)
}

func (h *handlers) mcpSetItemStatus(ctx context.Context, _ *mcp.CallToolRequest, in setItemStatusInput) (*mcp.CallToolResult, mcpItem, error) {
	item, err := h.mcpItem(ctx, in.ID, "")
	if err != nil {
		return nil, mcpItem{}, err
	}
	statusID, err := h.statusIDByName(ctx, item.WorkspaceID, strings.TrimSpace(in.Status))
	if err != nil {
		return nil, mcpItem{}, mcpErr(err)
	}
	if err := h.board.SetStatus(ctx, item.ID, statusID); err != nil {
		return nil, mcpItem{}, mcpErr(err)
	}
	h.liveUpsertOrigin(ctx, "", item.ID)
	return h.mcpReloadResult(ctx, item.ID)
}

func (h *handlers) mcpSetItemAssignee(ctx context.Context, _ *mcp.CallToolRequest, in setItemAssigneeInput) (*mcp.CallToolResult, mcpItem, error) {
	item, err := h.mcpItem(ctx, in.ID, "")
	if err != nil {
		return nil, mcpItem{}, err
	}
	assigneeID := ""
	if name := strings.TrimSpace(in.Assignee); name != "" {
		if strings.EqualFold(name, "me") {
			assigneeID = principalFrom(ctx).ID
		} else if assigneeID, err = h.userIDByName(ctx, name); err != nil {
			return nil, mcpItem{}, mcpErr(err)
		}
	}
	if err := h.board.SetAssignee(ctx, item.ID, assigneeID); err != nil {
		return nil, mcpItem{}, mcpErr(err)
	}
	h.liveUpsertOrigin(ctx, "", item.ID)
	return h.mcpReloadResult(ctx, item.ID)
}

func (h *handlers) mcpSetItemDescription(ctx context.Context, _ *mcp.CallToolRequest, in setItemDescriptionInput) (*mcp.CallToolResult, mcpItem, error) {
	item, err := h.mcpItem(ctx, in.ID, "")
	if err != nil {
		return nil, mcpItem{}, err
	}
	if err := h.board.UpdateDescription(ctx, item.ID, in.Description); err != nil {
		return nil, mcpItem{}, mcpErr(err)
	}
	return h.mcpReloadResult(ctx, item.ID)
}

func (h *handlers) mcpSetItemMilestone(ctx context.Context, _ *mcp.CallToolRequest, in setItemMilestoneInput) (*mcp.CallToolResult, mcpItem, error) {
	item, err := h.mcpItem(ctx, in.ID, "")
	if err != nil {
		return nil, mcpItem{}, err
	}
	if err := h.board.SetMilestone(ctx, item.ID, in.Milestone); err != nil {
		return nil, mcpItem{}, mcpErr(err)
	}
	h.liveUpsertOrigin(ctx, "", item.ID)
	return h.mcpReloadResult(ctx, item.ID)
}

func (h *handlers) mcpSetItemParent(ctx context.Context, _ *mcp.CallToolRequest, in setItemParentInput) (*mcp.CallToolResult, mcpItem, error) {
	item, err := h.mcpItem(ctx, in.ID, "")
	if err != nil {
		return nil, mcpItem{}, err
	}
	parent := strings.ToLower(strings.TrimSpace(in.Parent))
	if parent != "" {
		// Resolve in the item's workspace so a cross-workspace or missing parent
		// reads as "item not found" rather than a vaguer reparent error.
		if _, err := h.mcpItem(ctx, parent, item.WorkspaceID); err != nil {
			return nil, mcpItem{}, err
		}
	}
	if err := h.board.Reparent(ctx, item.ID, parent); err != nil {
		return nil, mcpItem{}, mcpErr(err)
	}
	h.liveUpsertOrigin(ctx, "", item.ID)
	return h.mcpReloadResult(ctx, item.ID)
}

func (h *handlers) mcpAddComment(ctx context.Context, _ *mcp.CallToolRequest, in addCommentInput) (*mcp.CallToolResult, commentAPI, error) {
	item, err := h.mcpItem(ctx, in.ID, "")
	if err != nil {
		return nil, commentAPI{}, err
	}
	p := principalFrom(ctx)
	c, notified, err := h.board.AddComment(ctx, item.ID, p.ID, in.Body)
	if err != nil {
		return nil, commentAPI{}, mcpErr(err)
	}
	h.publishLive(wsTopic(item.WorkspaceID), "comment.add", "", map[string]any{
		"item":   item.ID,
		"author": p.Display,
		"body":   c.Body,
		"at":     formatWhen(c.CreatedAt),
	})
	h.publishNotifications(ctx, notified)
	return &mcp.CallToolResult{}, commentAPI{
		Author: p.Username,
		Body:   c.Body,
		At:     c.CreatedAt.Format(time.RFC3339),
	}, nil
}

func (h *handlers) mcpArchiveItem(ctx context.Context, _ *mcp.CallToolRequest, in itemIDInput) (*mcp.CallToolResult, mcpItem, error) {
	item, err := h.mcpItem(ctx, in.ID, "")
	if err != nil {
		return nil, mcpItem{}, err
	}
	if err := h.board.Archive(ctx, item.ID); err != nil {
		return nil, mcpItem{}, mcpErr(err)
	}
	h.publishItemRemove("", item.WorkspaceID, item.ID)
	return h.mcpReloadResult(ctx, item.ID)
}

func (h *handlers) mcpUnarchiveItem(ctx context.Context, _ *mcp.CallToolRequest, in itemIDInput) (*mcp.CallToolResult, mcpItem, error) {
	item, err := h.mcpItem(ctx, in.ID, "")
	if err != nil {
		return nil, mcpItem{}, err
	}
	if err := h.board.Unarchive(ctx, item.ID); err != nil {
		return nil, mcpItem{}, mcpErr(err)
	}
	h.liveUpsertOrigin(ctx, "", item.ID)
	return h.mcpReloadResult(ctx, item.ID)
}

func (h *handlers) mcpListActivity(ctx context.Context, _ *mcp.CallToolRequest, in listActivityInput) (*mcp.CallToolResult, activityOutput, error) {
	ws, err := h.mcpWorkspace(ctx, in.Workspace)
	if err != nil {
		return nil, activityOutput{}, err
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 50
	}
	var events []store.Event
	if item := strings.TrimSpace(in.Item); item != "" {
		// Scope to one item, but keep it inside the named workspace.
		it, ierr := h.mcpItem(ctx, item, ws.ID)
		if ierr != nil {
			return nil, activityOutput{}, ierr
		}
		events, err = h.board.ItemHistory(ctx, it.ID, limit)
	} else {
		events, err = h.board.WorkspaceActivity(ctx, ws.ID, limit)
	}
	if err != nil {
		return nil, activityOutput{}, mcpErr(err)
	}
	out := activityOutput{Events: make([]mcpEvent, len(events))}
	for i, e := range events {
		out.Events[i] = mcpEvent{
			Actor:     e.ActorName,
			Verb:      e.Verb,
			Summary:   humanizeEvent(e),
			ItemID:    e.ItemID,
			ItemTitle: e.ItemTitle,
			Data:      e.Data,
			At:        e.CreatedAt.Format(time.RFC3339),
		}
	}
	return &mcp.CallToolResult{}, out, nil
}

func (h *handlers) mcpListNotifications(ctx context.Context, _ *mcp.CallToolRequest, in listNotificationsInput) (*mcp.CallToolResult, notificationsOutput, error) {
	p := principalFrom(ctx)
	if p == nil {
		return nil, notificationsOutput{}, errors.New("not authenticated")
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 50
	}
	var notes []store.Notification
	var err error
	if in.IncludeRead {
		notes, err = h.board.Notifications(ctx, p.ID, limit)
	} else {
		notes, err = h.board.UnreadNotifications(ctx, p.ID, limit)
	}
	if err != nil {
		return nil, notificationsOutput{}, mcpErr(err)
	}
	unread, err := h.board.UnreadCount(ctx, p.ID)
	if err != nil {
		return nil, notificationsOutput{}, mcpErr(err)
	}
	out := notificationsOutput{Notifications: make([]mcpNotification, 0, len(notes)), Unread: unread}
	for _, n := range notes {
		out.Notifications = append(out.Notifications, mcpNotification{
			ID:        n.ID,
			Kind:      n.Kind,
			Unread:    n.ReadAt == nil,
			Actor:     n.ActorName,
			Workspace: n.WorkspaceSlug,
			ItemID:    n.ItemID,
			ItemTitle: n.ItemTitle,
			Excerpt:   n.Excerpt,
			URL:       h.itemURL(n.WorkspaceSlug, n.ItemID),
			At:        n.CreatedAt.Format(time.RFC3339),
		})
	}
	return &mcp.CallToolResult{}, out, nil
}

func (h *handlers) mcpMarkNotificationRead(ctx context.Context, _ *mcp.CallToolRequest, in markNotificationReadInput) (*mcp.CallToolResult, markReadOutput, error) {
	p := principalFrom(ctx)
	if p == nil {
		return nil, markReadOutput{}, errors.New("not authenticated")
	}
	// Scoped to the caller, so one principal can't clear another's inbox. The
	// mark is idempotent — an unknown or already-read id is a silent no-op — so
	// the only useful signal back is the remaining unread count.
	if err := h.board.MarkNotificationRead(ctx, strings.TrimSpace(in.ID), p.ID); err != nil {
		return nil, markReadOutput{}, mcpErr(err)
	}
	unread, err := h.board.UnreadCount(ctx, p.ID)
	if err != nil {
		return nil, markReadOutput{}, mcpErr(err)
	}
	return &mcp.CallToolResult{}, markReadOutput{Unread: unread}, nil
}

// --- shared helpers ---

// mcpWorkspace resolves a workspace slug, mapping a miss to a clean error.
func (h *handlers) mcpWorkspace(ctx context.Context, slug string) (store.Workspace, error) {
	ws, err := h.workspaces.BySlug(ctx, strings.ToLower(strings.TrimSpace(slug)))
	if err != nil {
		return store.Workspace{}, mcpErr(err)
	}
	return ws, nil
}

// mcpItem resolves an item id. When workspaceID is non-empty the item must
// belong to it, otherwise it reads as not-found (no cross-workspace leakage).
func (h *handlers) mcpItem(ctx context.Context, id, workspaceID string) (store.Item, error) {
	id = strings.TrimSpace(id)
	it, err := h.board.Item(ctx, strings.ToLower(id))
	// Fall back to a human reference (PREFIX-N): the prefix names the workspace,
	// the number the item within it. Bare numbers aren't accepted here — without
	// a prefix there's no workspace to scope them to.
	if errors.Is(err, store.ErrItemNotFound) {
		if prefix, num, ok := parseItemRef(id); ok && prefix != "" {
			if ws, werr := h.workspaces.ByPrefix(ctx, prefix); werr == nil {
				it, err = h.board.ItemByRef(ctx, ws.ID, num)
			}
		}
	}
	if err != nil {
		return store.Item{}, mcpErr(err)
	}
	if workspaceID != "" && it.WorkspaceID != workspaceID {
		return store.Item{}, errors.New("item not found")
	}
	return it, nil
}

// resolveAssignee turns the assignee/mine inputs into an id and whether to
// filter. mine (or assignee="me") targets the caller; a username is looked up.
func (h *handlers) resolveAssignee(ctx context.Context, assignee string, mine bool) (id string, filter bool, err error) {
	if mine {
		return principalFrom(ctx).ID, true, nil
	}
	name := strings.TrimSpace(assignee)
	if name == "" {
		return "", false, nil
	}
	if strings.EqualFold(name, "me") {
		return principalFrom(ctx).ID, true, nil
	}
	id, err = h.userIDByName(ctx, name)
	return id, true, err
}

// mcpItemResult renders a freshly returned item as a tool result.
func (h *handlers) mcpItemResult(ctx context.Context, it store.Item) (*mcp.CallToolResult, mcpItem, error) {
	statusName, userName, err := h.nameMaps(ctx, it.WorkspaceID)
	if err != nil {
		return nil, mcpItem{}, mcpErr(err)
	}
	v := toMCPItem(it, statusName, userName, h.prefixFor(ctx, it.WorkspaceID))
	v.URL = h.itemURL(h.slugFor(ctx, it.WorkspaceID), it.ID)
	return &mcp.CallToolResult{}, v, nil
}

// mcpReloadResult reloads an item by id (after a mutation) and renders it.
func (h *handlers) mcpReloadResult(ctx context.Context, id string) (*mcp.CallToolResult, mcpItem, error) {
	it, err := h.board.Item(ctx, id)
	if err != nil {
		return nil, mcpItem{}, mcpErr(err)
	}
	return h.mcpItemResult(ctx, it)
}

// mcpErr maps board/store errors to clean, agent-facing messages. Name-lookup
// errors are already human-readable (and name the bad value), so they pass
// through; unexpected errors are logged and reduced to a generic message.
func mcpErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, store.ErrWorkspaceNotFound):
		return errors.New("workspace not found")
	case errors.Is(err, store.ErrItemNotFound):
		return errors.New("item not found")
	case errors.Is(err, store.ErrStatusNotFound), errors.Is(err, board.ErrStatusMismatch):
		return errors.New("invalid status")
	case errors.Is(err, board.ErrInvalidTitle):
		return errors.New("invalid title")
	case errors.Is(err, board.ErrInvalidComment):
		return errors.New("invalid comment: empty or too long")
	case errors.Is(err, board.ErrInvalidDescription):
		return errors.New("description too long")
	case errors.Is(err, board.ErrCycle):
		return errors.New("would create a cycle: an item can't be parented under itself or a descendant")
	case errors.Is(err, board.ErrNoStatus):
		return errors.New("workspace has no statuses")
	case errors.Is(err, errUnknownStatus), errors.Is(err, errUnknownUser):
		return err
	default:
		slog.Error("mcp tool error", "err", err)
		return fmt.Errorf("internal error")
	}
}
