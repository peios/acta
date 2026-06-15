package web

import (
	"context"
	"encoding/json"
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
	streamable := mcp.NewStreamableHTTPHandler(
		func(r *http.Request) *mcp.Server {
			srv := mcp.NewServer(&mcp.Implementation{Name: "acta", Version: "v1"}, nil)
			h.registerMCPTools(srv)
			h.registerMCPResources(r.Context(), srv)
			h.registerMCPPrompts(r.Context(), srv)
			return srv
		},
		&mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true},
	)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// MCP tools can block: watch_comments long-polls until a comment lands or
		// its window lapses. The server's short global WriteTimeout would sever
		// such a held request mid-call — the client sees a 502, not the clean
		// empty response it should loop on. Extend this request's write deadline
		// to cover the longest tool block plus margin, the same deadline
		// management the SSE endpoint does on each heartbeat.
		_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(mcpWriteHeadroom))
		streamable.ServeHTTP(w, r)
	})
}

// mcpWriteHeadroom is the per-request write deadline for the /mcp endpoint:
// comfortably above watch_comments' max block (30s) and below the upstream
// proxy/CDN limits (nginx 60s, Cloudflare 100s).
const mcpWriteHeadroom = 45 * time.Second

func (h *handlers) registerMCPTools(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "whoami",
		Description: "Return the principal the current token authenticates as. is_agent is true for an agent (username owner/agentname), with owner naming the human it acts for.",
	}, h.mcpWhoami)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_principals",
		Description: "List assignable principals visible to the caller: active humans, plus the caller's own agents. Use this before assigning work when you don't know the exact username.",
	}, h.mcpListPrincipals)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_workspaces",
		Description: "List the workspaces. Each has a slug (used to address it in other tools) and a display name. A workspace contains boards (see list_boards) — Tasks and Backlog.",
	}, h.mcpListWorkspaces)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_boards",
		Description: "List a workspace's boards (e.g. Tasks and Backlog). Each has a slug to pass as the `board` argument on list_statuses, list_items, create_item and list_activity. The first board is the primary one those tools default to.",
	}, h.mcpListBoards)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_statuses",
		Description: "List a board's status lanes, in order. Statuses are addressed by name (in create_item, set_item_status, and the list_items status filter). Defaults to the primary board; pass board (a slug from list_boards) for another. The first lane is a board's entry lane (where new items land). A lane may carry required_facts — a checklist you must confirm (via set_item_status's checklist) before an item can enter it.",
	}, h.mcpListStatuses)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_items",
		Description: "List a board's items. Defaults to the primary board; pass board (a slug from list_boards) for another. Returns top-level items by default; pass parent to list the direct subtasks of an item instead. Optional filters narrow by status (lane name), assignee (username, or \"me\"), mine, project, release, priority, type, size, and overdue. Statuses and principals are named, not id-addressed; items are addressed by id.",
	}, h.mcpListItems)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_item",
		Description: "Fetch one item by id with its full context: status, assignee, author, parent, its direct subtasks, and all comments oldest-first.",
	}, h.mcpGetItem)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "create_item",
		Description: "Create an item. Defaults to the primary board; pass board (a slug from list_boards) to create it on another (e.g. Backlog). Provide a status lane by name (defaults to the board's entry lane) or a parent item id to create it as a subtask. Optionally set priority (low/medium/high/urgent), type (feature/bug/chore), size (xs–xl), and a due date (YYYY-MM-DD). The item is attributed to the calling principal.",
	}, h.mcpCreateItem)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "set_item_status",
		Description: "Move an item to a different status lane, named (e.g. \"Doing\", \"Done\"). If the target lane has a checklist (see required_facts on list_statuses), pass the fact titles you confirm are true as `checklist` — the move is rejected, naming what's still required, until the checklist is satisfied (already-true facts carry over and needn't be repeated).",
	}, h.mcpSetItemStatus)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "claim_item",
		Description: "Claim an item as the caller: assign it to yourself, optionally move it to a named status, optionally confirming checklist facts, and optionally add a progress comment. Useful when starting work on an item.",
	}, h.mcpClaimItem)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "set_item_title",
		Description: "Rename an item by id. The title is trimmed and must be non-empty.",
	}, h.mcpSetItemTitle)

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
		Name:        "set_item_priority",
		Description: "Set an item's priority: low, medium, high, or urgent. Omit priority to clear it.",
	}, h.mcpSetItemPriority)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "set_item_type",
		Description: "Set an item's type: feature, bug, or chore. Omit type to clear it.",
	}, h.mcpSetItemType)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "set_item_size",
		Description: "Set an item's size estimate: xs, s, m, l, or xl. Omit size to clear it.",
	}, h.mcpSetItemSize)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "set_item_due",
		Description: "Set an item's due date (YYYY-MM-DD). Omit due to clear it.",
	}, h.mcpSetItemDue)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "set_item_parent",
		Description: "Reparent an item: pass a parent item id to nest it under that item (same workspace), or omit parent to promote it to a top-level board item. An item can't be parented under itself or one of its own descendants.",
	}, h.mcpSetItemParent)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_projects",
		Description: "List a workspace's projects: cross-cutting initiatives that group items (e.g. all \"Peinit\" work). Each has a slug (used to address it in create_item, set_item_project, and the list_items project filter), a name, a lifecycle status (planned/active/paused/done), an optional lead, and progress (done/total top-level items).",
	}, h.mcpListProjects)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "create_project",
		Description: "Create a project in a workspace to group related items. Provide a name; optionally a brief (markdown), a status (defaults to active), and a lead (a username, or \"me\"). Returns the new project, including its slug. File items under it with set_item_project (or create_item's project argument).",
	}, h.mcpCreateProject)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "set_item_project",
		Description: "File an item under a project, by project slug (from list_projects). Omit project to remove the item from its project. A project groups an item's work independently of its board, lane, and parent.",
	}, h.mcpSetItemProject)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_releases",
		Description: "List a workspace's releases: versioned cut-lines items ship in (e.g. \"v0.27.0\"). Each has a name (used to address it in create_item, set_item_release, set_release_status, and the list_items release filter), a lifecycle status (planned/active/shipped), a ship time once shipped, and progress (done/total top-level items). A release differs from a project: it's a point the project ships at, not an open-ended theme.",
	}, h.mcpListReleases)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "create_release",
		Description: "Create a release in a workspace — a versioned cut-line to gather work toward. Provide a name (unique in the workspace); optionally a description (markdown) and a status (planned, or active by default). Returns the new release. Add items with set_item_release (or create_item's release argument); ship it later with set_release_status.",
	}, h.mcpCreateRelease)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "set_item_release",
		Description: "Add an item to a release, by release name (from list_releases). Omit release to remove the item from its release. An item belongs to one release at a time.",
	}, h.mcpSetItemRelease)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "set_release_status",
		Description: "Move a release along its lifecycle: planned → active → shipped, and back. \"shipped\" stamps the ship time and freezes it as a changelog entry; moving away from shipped clears that stamp. Addressed by release name.",
	}, h.mcpSetReleaseStatus)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_subscriptions",
		Description: "List your subscriptions — the standing interests that file activity notifications into your inbox. Each names a subject (type item/project/principal, addressed by ref: an item id, a project slug, or a username) and the category filter (comments, status, assignments, items_added, other). You auto-subscribe to items you create/comment on/are assigned, projects you create, and your own agents.",
	}, h.mcpListSubscriptions)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "subscribe",
		Description: "Follow a subject so its activity files notifications into your inbox (poll with list_notifications). type is item|project|principal; ref is the natural key — an item id, a project slug (pass workspace too), or a username (\"me\" for yourself). Optionally set events to choose categories (comments, status, assignments, items_added, other) — e.g. all five to watch everything a principal does; omit to use the type default (item: comments+status, project: items_added+status, principal: status) without disturbing an existing subscription. Idempotent.",
	}, h.mcpSubscribe)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "unsubscribe",
		Description: "Stop following a subject: type is item|project|principal and ref its natural key (item id, project slug with workspace, or username). Idempotent — removing a subscription you don't hold is a no-op.",
	}, h.mcpUnsubscribe)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "add_comment",
		Description: "Append a comment to an item, authored by the calling principal. Comments are how agents record progress and coordinate. Returns the new comment, including its id (use it as the `after` cursor for watch_comments).",
	}, h.mcpAddComment)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_documents",
		Description: "List the documents attached to an item — titled long-form markdown artifacts (compliance reports, findings, runbooks), distinct from the item's description and its comment thread. Returns each document's id, title, author and timestamps, but not its body; fetch a body with get_document. An item can hold many documents.",
	}, h.mcpListDocuments)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_document",
		Description: "Fetch one document by id (from list_documents), including its full markdown body.",
	}, h.mcpGetDocument)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "create_document",
		Description: "Attach a new markdown document to an item: a titled long-form artifact like a compliance report or findings doc. Provide the item id, a title, and the body (markdown). Returns the created document. Use this for substantial reports you want to keep on the task; use set_item_description for the task's primary body and add_comment for short progress notes.",
	}, h.mcpCreateDocument)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "update_document",
		Description: "Replace a document's title and body in place, by document id (from list_documents). Both are required and overwrite the old values.",
	}, h.mcpUpdateDocument)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "delete_document",
		Description: "Delete a document by id (from list_documents). This is permanent.",
	}, h.mcpDeleteDocument)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "watch_comments",
		Description: "Block until new comments are posted on an item, then return them — the way to wait on a human (or another agent) replying in a thread. Returns every comment after the `after` cursor (a comment id from add_comment or a prior watch; omit to watch for comments posted from now on). Waits up to ~25s for at least one and returns the moment they arrive, or an empty list on timeout so you can call again. To ask and wait for an answer: add_comment(your question), then loop watch_comments with `after` set to that comment's id, advancing `after` to the returned cursor each call until you get a reply you can act on.",
	}, h.mcpWatchComments)

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
		Description: "Read the activity log, newest first: who changed what and when (creations, status moves, assignments, comments, archives, …). Pass item for one item's history, board (a slug from list_boards) for one board's feed, or omit both for the whole workspace. Use this to answer \"what changed since yesterday\" for a standup instead of diffing the board. Each entry has a human-readable summary plus the raw verb and data for precise parsing.",
	}, h.mcpListActivity)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_notifications",
		Description: "Poll your notification inbox, newest first. Returns unread notifications by default — the set to drain — so an idle agent can long-poll this to learn when a human @mentions it in a comment. Set include_read to also list ones already marked read. Each entry names the actor, the item it points at (id, workspace slug, permalink url), and an excerpt of the comment. Act on one, then call mark_notification_read with its id.",
	}, h.mcpListNotifications)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "mark_notification_read",
		Description: "Mark one of your notifications read by id (ids come from list_notifications), clearing it from the unread inbox. Idempotent: an already-read, unknown, or someone else's id is a no-op. Returns your remaining unread count.",
	}, h.mcpMarkNotificationRead)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "memory_recall",
		Description: "Recall your durable markdown memories. Call this at the start of a task to remember what you already know. With no arguments it returns everything visible to you across scopes — agent (your own), user (your owner's), and site (instance-wide) — as a scannable index (name + one-line summary), no bodies. Pass workspace (a slug) to also include that workspace's shared memories, and project (a slug, with workspace) for a project's. Filter with scopes and a query substring; set include_bodies to get full markdown inline. Then memory_get the ones worth reading in full.",
	}, h.mcpMemoryRecall)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "memory_get",
		Description: "Read one memory's full markdown body. Address it by scope + name (the usual way) or by id. For workspace scope pass the workspace slug; for project scope pass workspace + project slugs.",
	}, h.mcpMemoryGet)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "memory_save",
		Description: "Save a durable memory — create it, or overwrite/append by name (upsert on scope+name, so you never juggle ids). Use this when you learn something worth remembering across sessions: a convention, a decision, a gotcha, a preference. scope is agent (your own scratchpad), user (your owner), site (instance-wide), workspace, or project. Give a short name (the key), a one-line summary (shown in recall), and the markdown body. mode defaults to replace; use append to add to an existing memory's body. workspace/project slugs are required for those scopes.",
	}, h.mcpMemorySave)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "memory_edit",
		Description: "Make a surgical edit to a memory's body: replace old_string with new_string (like a file edit), without rewriting the whole thing. old_string must match exactly once unless replace_all is set. Address the memory by scope + name (+ workspace/project slugs where the scope needs them).",
	}, h.mcpMemoryEdit)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "memory_delete",
		Description: "Delete a memory you no longer want, by scope + name (or by id). Use when a memory is wrong or stale — prefer fixing it with memory_edit/memory_save when it's merely out of date.",
	}, h.mcpMemoryDelete)
}

// --- tool input/output types ---

type emptyInput struct{}

type principalView struct {
	Username string `json:"username"`
	Display  string `json:"display,omitempty"`
	IsAgent  bool   `json:"is_agent"`
	Owner    string `json:"owner,omitempty"` // owning human, when is_agent
}

type principalListOutput struct {
	Principals []principalView `json:"principals"`
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
	Project       string `json:"project,omitempty"`  // project slug, "" if unfiled
	Release       string `json:"release,omitempty"`  // release name, "" if in none
	Priority      string `json:"priority,omitempty"` // low|medium|high|urgent, "" if unset
	Type          string `json:"type,omitempty"`     // feature|bug|chore, "" if unset
	Size          string `json:"size,omitempty"`     // xs|s|m|l|xl, "" if unset
	Due           string `json:"due,omitempty"`      // YYYY-MM-DD, "" if no due date
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

// commentAPI is a comment as the MCP surface presents it: its id (a cursor for
// watch_comments), author by username, body, and an RFC3339 timestamp.
type commentAPI struct {
	ID     string `json:"id,omitempty"`
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

type listBoardsInput struct {
	Workspace string `json:"workspace" jsonschema:"slug of the workspace whose boards to list"`
}

// boardAPI is a board as the MCP surface presents it: a slug to pass as the
// `board` argument on other tools, and a display name.
type boardAPI struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

type boardListOutput struct {
	Boards []boardAPI `json:"boards"`
}

// statusAPI is a board lane as the MCP surface presents it: the name agents
// address it by, and its zero-based board position (position 0 is the first
// lane, the default for new items). Colour is omitted as UI-only. RequiredFacts
// are the checklist facts an item must have confirmed to enter this lane (empty
// for an ungated lane) — pass them as set_item_status's `checklist`.
type statusAPI struct {
	Name          string   `json:"name"`
	Position      int      `json:"position"`
	RequiredFacts []string `json:"required_facts,omitempty"`
}

type itemListOutput struct {
	Items []mcpItem `json:"items"`
}

func toMCPItem(it store.Item, statusName, userName, projectSlug map[string]string, prefix string) mcpItem {
	return mcpItem{
		ID:        it.ID,
		Ref:       refID(prefix, it.RefNum),
		Title:     it.Title,
		Status:    statusName[it.StatusID],
		Assignee:  userName[it.AssigneeID],
		Project:   projectSlug[it.ProjectID],
		Priority:  attrSlugOut(board.Priorities, it.Priority),
		Type:      attrSlugOut(board.ItemTypes, it.Type),
		Size:      attrSlugOut(board.Sizes, it.Size),
		Due:       board.DueString(it.DueDate),
		Milestone: it.IsMilestone,
		Archived:  it.ArchivedAt != nil,
		CreatedBy: userName[it.CreatedBy],
		CreatedAt: it.CreatedAt.Format(time.RFC3339),
		ParentID:  it.ParentID,
	}
}

// attrSlugOut is the wire slug for an enum attribute, or "" for unset (0) so the
// omitempty field drops out rather than serialising "none".
func attrSlugOut(v board.AttrVocab, value int) string {
	if value == 0 {
		return ""
	}
	return v.Slug(value)
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
	Board     string `json:"board,omitempty" jsonschema:"board slug (from list_boards); defaults to the primary board"`
}

type listItemsInput struct {
	Workspace       string `json:"workspace" jsonschema:"slug of the workspace to list"`
	Board           string `json:"board,omitempty" jsonschema:"board slug (from list_boards); defaults to the primary board. Pass * to span every board, including a Backlog that an unscoped search would otherwise skip. Ignored when parent is set"`
	Status          string `json:"status,omitempty" jsonschema:"only items in this status lane, by name"`
	Assignee        string `json:"assignee,omitempty" jsonschema:"only items assigned to this username; use \"me\" for the caller"`
	Project         string `json:"project,omitempty" jsonschema:"only items filed under this project (a slug from list_projects)"`
	Release         string `json:"release,omitempty" jsonschema:"only items in this release (a name from list_releases)"`
	Priority        string `json:"priority,omitempty" jsonschema:"only items with this priority: low, medium, high, urgent, or none (unset)"`
	Type            string `json:"type,omitempty" jsonschema:"only items of this type: feature, bug, chore, or none (unset)"`
	Size            string `json:"size,omitempty" jsonschema:"only items of this size: xs, s, m, l, xl, or none (unset)"`
	Overdue         bool   `json:"overdue,omitempty" jsonschema:"only items past their due date and not done"`
	Parent          string `json:"parent,omitempty" jsonschema:"list the direct subtasks of this item id instead of the board's top-level items"`
	Mine            bool   `json:"mine,omitempty" jsonschema:"only items assigned to the calling principal (shorthand for assignee=me)"`
	Query           string `json:"q,omitempty" jsonschema:"free-text search: a case-insensitive substring of the title or description, matched at every subtask depth within the scope. The scope is the board (default: your primary board, so Backlog is skipped; a named board; or * for every board) or, with parent set, that item's children. Results rank by relevance and an exact human id like ACTA-12 floats to the top. The status/assignee/project/release filters still narrow"`
	IncludeArchived bool   `json:"include_archived,omitempty" jsonschema:"include archived items (only applies together with q)"`
}

type itemIDInput struct {
	ID string `json:"id" jsonschema:"the item id"`
}

type createItemInput struct {
	Workspace string `json:"workspace" jsonschema:"slug of the workspace to create the item in"`
	Title     string `json:"title" jsonschema:"the item title"`
	Board     string `json:"board,omitempty" jsonschema:"board slug (from list_boards); defaults to the primary board. Ignored when parent is set"`
	Status    string `json:"status,omitempty" jsonschema:"status lane by name (on the chosen board); defaults to the board's entry lane. Ignored when parent is set"`
	Parent    string `json:"parent,omitempty" jsonschema:"parent item id; when set, create this as a subtask of that item"`
	Project   string `json:"project,omitempty" jsonschema:"file the new item under this project (a slug from list_projects)"`
	Release   string `json:"release,omitempty" jsonschema:"add the new item to this release (a name from list_releases)"`
	Priority  string `json:"priority,omitempty" jsonschema:"priority: low, medium, high, or urgent"`
	Type      string `json:"type,omitempty" jsonschema:"type: feature, bug, or chore"`
	Size      string `json:"size,omitempty" jsonschema:"size estimate: xs, s, m, l, or xl"`
	Due       string `json:"due,omitempty" jsonschema:"due date as YYYY-MM-DD"`
}

type listProjectsInput struct {
	Workspace string `json:"workspace" jsonschema:"slug of the workspace whose projects to list"`
}

type projectListOutput struct {
	Projects []projectAPI `json:"projects"`
}

type createProjectInput struct {
	Workspace string `json:"workspace" jsonschema:"slug of the workspace to create the project in"`
	Name      string `json:"name" jsonschema:"the project name"`
	Brief     string `json:"brief,omitempty" jsonschema:"a short description of the project (markdown)"`
	Status    string `json:"status,omitempty" jsonschema:"lifecycle: planned, active (default), paused, or done"`
	Lead      string `json:"lead,omitempty" jsonschema:"username of the project lead; \"me\" for the caller"`
}

type setItemProjectInput struct {
	ID      string `json:"id" jsonschema:"the item id"`
	Project string `json:"project,omitempty" jsonschema:"project slug (from list_projects) to file the item under; omit to remove it from its project"`
}

type listReleasesInput struct {
	Workspace string `json:"workspace" jsonschema:"slug of the workspace whose releases to list"`
}

type releaseListOutput struct {
	Releases []releaseAPI `json:"releases"`
}

type createReleaseInput struct {
	Workspace   string `json:"workspace" jsonschema:"slug of the workspace to create the release in"`
	Name        string `json:"name" jsonschema:"the release name (e.g. \"v0.27.0\"), unique within the workspace"`
	Description string `json:"description,omitempty" jsonschema:"notes about the release (markdown)"`
	Status      string `json:"status,omitempty" jsonschema:"lifecycle to create it in: planned, or active (default). shipped is reached later via set_release_status"`
}

type setItemReleaseInput struct {
	ID      string `json:"id" jsonschema:"the item id"`
	Release string `json:"release,omitempty" jsonschema:"release name (from list_releases) to add the item to; omit to remove it from its release"`
}

type setReleaseStatusInput struct {
	Workspace string `json:"workspace" jsonschema:"slug of the workspace the release is in"`
	Release   string `json:"release" jsonschema:"the release name (from list_releases)"`
	Status    string `json:"status" jsonschema:"new lifecycle state: planned, active, or shipped (shipping stamps the ship time and freezes it)"`
}

type subscribeInput struct {
	Type      string   `json:"type" jsonschema:"what to follow: item, project, or principal"`
	Ref       string   `json:"ref" jsonschema:"the subject's natural key: an item id, a project slug, or a username (\"me\" for yourself)"`
	Workspace string   `json:"workspace,omitempty" jsonschema:"workspace slug — required when type is project (slugs are per-workspace)"`
	Events    []string `json:"events,omitempty" jsonschema:"category filter to set: comments, status, assignments, items_added, other. Omit to use the type default on a new subscription and leave an existing one unchanged"`
}

type unsubscribeInput struct {
	Type      string `json:"type" jsonschema:"item, project, or principal"`
	Ref       string `json:"ref" jsonschema:"the subject's natural key: an item id, a project slug, or a username"`
	Workspace string `json:"workspace,omitempty" jsonschema:"workspace slug — required when type is project"`
}

type subscriptionListOutput struct {
	Subscriptions []subscriptionAPI `json:"subscriptions"`
}

type unsubscribeOutput struct {
	OK bool `json:"ok"`
}

type setItemStatusInput struct {
	ID        string   `json:"id" jsonschema:"the item id"`
	Status    string   `json:"status" jsonschema:"target status lane, by name"`
	Checklist []string `json:"checklist,omitempty" jsonschema:"fact titles you confirm are true, to pass the lane's checklist gate (see required_facts on list_statuses). Each is recorded as confirmed by you. Only needed when the target lane is gated; already-true facts needn't be repeated"`
}

type claimItemInput struct {
	ID        string   `json:"id" jsonschema:"the item id to claim"`
	Status    string   `json:"status,omitempty" jsonschema:"optional status lane to move the item to after claiming it"`
	Checklist []string `json:"checklist,omitempty" jsonschema:"fact titles you confirm are true if the target status is gated"`
	Comment   string   `json:"comment,omitempty" jsonschema:"optional progress comment to add after claiming"`
}

type setItemTitleInput struct {
	ID    string `json:"id" jsonschema:"the item id"`
	Title string `json:"title" jsonschema:"the new title"`
}

type setItemAssigneeInput struct {
	ID       string `json:"id" jsonschema:"the item id"`
	Assignee string `json:"assignee,omitempty" jsonschema:"username to assign to; \"me\" for the caller; omit to clear the assignment"`
}

type addCommentInput struct {
	ID   string `json:"id" jsonschema:"the item id to comment on"`
	Body string `json:"body" jsonschema:"the comment text"`
}

type watchCommentsInput struct {
	Item           string `json:"item" jsonschema:"the item id whose comments to watch"`
	After          string `json:"after,omitempty" jsonschema:"return comments after this comment id (from add_comment or a prior watch's cursor); omit to watch for comments posted from now on"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty" jsonschema:"max seconds to block waiting for a comment (default 25, max 30); on timeout the comments list is empty so you can call again"`
}

type watchCommentsOutput struct {
	Comments []commentAPI `json:"comments"`
	// Cursor is the id to pass as `after` on the next call: the newest returned
	// comment, or the unchanged cursor when the wait timed out with none.
	Cursor string `json:"cursor"`
}

type setItemDescriptionInput struct {
	ID          string `json:"id" jsonschema:"the item id"`
	Description string `json:"description" jsonschema:"the new description; empty clears it"`
}

type setItemMilestoneInput struct {
	ID        string `json:"id" jsonschema:"the item id"`
	Milestone bool   `json:"milestone" jsonschema:"true to flag as a milestone, false to unflag"`
}

type setItemPriorityInput struct {
	ID       string `json:"id" jsonschema:"the item id"`
	Priority string `json:"priority,omitempty" jsonschema:"low, medium, high, or urgent; omit to clear the priority"`
}

type setItemTypeInput struct {
	ID   string `json:"id" jsonschema:"the item id"`
	Type string `json:"type,omitempty" jsonschema:"feature, bug, or chore; omit to clear the type"`
}

type setItemSizeInput struct {
	ID   string `json:"id" jsonschema:"the item id"`
	Size string `json:"size,omitempty" jsonschema:"xs, s, m, l, or xl; omit to clear the size"`
}

type setItemDueInput struct {
	ID  string `json:"id" jsonschema:"the item id"`
	Due string `json:"due,omitempty" jsonschema:"due date as YYYY-MM-DD; omit to clear the due date"`
}

type setItemParentInput struct {
	ID     string `json:"id" jsonschema:"the item id to reparent"`
	Parent string `json:"parent,omitempty" jsonschema:"new parent item id; omit to promote to a top-level board item"`
}

type listActivityInput struct {
	Workspace string `json:"workspace" jsonschema:"slug of the workspace whose activity to read"`
	Board     string `json:"board,omitempty" jsonschema:"board slug (from list_boards); scope the feed to one board. Ignored when item is set"`
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
	Kind      string `json:"kind"` // "mention" or "activity" (a subscription matched)
	Unread    bool   `json:"unread"`
	Actor     string `json:"actor,omitempty"`
	Workspace string `json:"workspace,omitempty"` // slug of the item's board
	ItemID    string `json:"item_id,omitempty"`
	ItemTitle string `json:"item_title,omitempty"`
	Verb      string `json:"verb,omitempty"`    // activity rows: the raw event verb, e.g. item.status_changed
	Summary   string `json:"summary,omitempty"` // activity rows: the rendered phrase, e.g. "moved to Done"
	Excerpt   string `json:"excerpt,omitempty"` // mention rows: the comment excerpt
	URL       string `json:"url,omitempty"`     // permalink to open the item on the board
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
	return &mcp.CallToolResult{}, principalViewFor(p.Username, p.Display), nil
}

func principalViewFor(username, display string) principalView {
	v := principalView{Username: username, Display: display}
	if owner, _, ok := strings.Cut(username, "/"); ok {
		v.IsAgent = true
		v.Owner = owner
	}
	return v
}

func (h *handlers) mcpListPrincipals(ctx context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, principalListOutput, error) {
	users, err := h.board.Assignables(ctx)
	if err != nil {
		return nil, principalListOutput{}, mcpErr(err)
	}
	out := principalListOutput{Principals: make([]principalView, len(users))}
	for i, u := range users {
		out.Principals[i] = principalViewFor(u.Username, u.Display)
	}
	return &mcp.CallToolResult{}, out, nil
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

// mcpBoard resolves the board a tool targets: the named board (by slug) or the
// workspace's primary board when the slug is blank. Agents discover slugs via
// list_boards.
func (h *handlers) mcpBoard(ctx context.Context, ws store.Workspace, slug string) (store.Board, error) {
	if slug = strings.ToLower(strings.TrimSpace(slug)); slug != "" {
		bd, err := h.board.BoardBySlug(ctx, ws.ID, slug)
		if err != nil {
			return store.Board{}, mcpErr(err)
		}
		return bd, nil
	}
	bd, err := h.board.DefaultBoard(ctx, ws.ID)
	if err != nil {
		return store.Board{}, mcpErr(err)
	}
	return bd, nil
}

func (h *handlers) mcpListBoards(ctx context.Context, _ *mcp.CallToolRequest, in listBoardsInput) (*mcp.CallToolResult, boardListOutput, error) {
	ws, err := h.mcpWorkspace(ctx, in.Workspace)
	if err != nil {
		return nil, boardListOutput{}, err
	}
	list, err := h.board.Boards(ctx, ws.ID)
	if err != nil {
		return nil, boardListOutput{}, mcpErr(err)
	}
	out := boardListOutput{Boards: make([]boardAPI, len(list))}
	for i, b := range list {
		out.Boards[i] = boardAPI{Slug: b.Slug, Name: b.Name}
	}
	return &mcp.CallToolResult{}, out, nil
}

func (h *handlers) mcpListStatuses(ctx context.Context, _ *mcp.CallToolRequest, in listStatusesInput) (*mcp.CallToolResult, statusListOutput, error) {
	ws, err := h.mcpWorkspace(ctx, in.Workspace)
	if err != nil {
		return nil, statusListOutput{}, err
	}
	bd, err := h.mcpBoard(ctx, ws, in.Board)
	if err != nil {
		return nil, statusListOutput{}, err
	}
	list, err := h.board.BoardStatuses(ctx, bd.ID)
	if err != nil {
		return nil, statusListOutput{}, mcpErr(err)
	}
	out := statusListOutput{Statuses: make([]statusAPI, len(list))}
	for i, s := range list {
		out.Statuses[i] = statusAPI{Name: s.Name, Position: s.Position}
		facts, ferr := h.board.StatusFacts(ctx, s.ID)
		if ferr != nil {
			return nil, statusListOutput{}, mcpErr(ferr)
		}
		for _, f := range facts {
			out.Statuses[i].RequiredFacts = append(out.Statuses[i].RequiredFacts, f.Title)
		}
	}
	return &mcp.CallToolResult{}, out, nil
}

func (h *handlers) mcpListItems(ctx context.Context, _ *mcp.CallToolRequest, in listItemsInput) (*mcp.CallToolResult, itemListOutput, error) {
	ws, err := h.mcpWorkspace(ctx, in.Workspace)
	if err != nil {
		return nil, itemListOutput{}, err
	}

	// board=* spans every board (Backlog included); otherwise the named board, or
	// the primary board by default. bd is the reference board for the status
	// filter and the done lane even when spanning — those stay scoped to it.
	allBoards := strings.TrimSpace(in.Board) == "*"
	var bd store.Board
	if allBoards {
		bd, err = h.board.DefaultBoard(ctx, ws.ID)
	} else {
		bd, err = h.mcpBoard(ctx, ws, in.Board)
	}
	if err != nil {
		return nil, itemListOutput{}, mcpErr(err)
	}
	boardStatuses, err := h.board.BoardStatuses(ctx, bd.ID)
	if err != nil {
		return nil, itemListOutput{}, mcpErr(err)
	}

	// Resolve filters up front so a bad name fails before we list. The status
	// filter is scoped to the reference board.
	statusID := ""
	if s := strings.TrimSpace(in.Status); s != "" {
		if statusID, err = statusIDInList(boardStatuses, s); err != nil {
			return nil, itemListOutput{}, mcpErr(err)
		}
	}
	assigneeID, filterAssignee, err := h.resolveAssignee(ctx, in.Assignee, in.Mine)
	if err != nil {
		return nil, itemListOutput{}, mcpErr(err)
	}

	// Base set. q narrows within the scope at every depth: a parent's children, a
	// single board, or — board=* — every board; default scope is the primary
	// board, so an unscoped search never reaches Backlog. Without q it's the
	// scope's top-level cards, as before.
	parent := strings.ToLower(strings.TrimSpace(in.Parent))
	query := strings.TrimSpace(in.Query)
	var items []store.Item
	switch {
	case query != "" && parent != "":
		if _, err := h.mcpItem(ctx, parent, ws.ID); err != nil {
			return nil, itemListOutput{}, err
		}
		kids, kerr := h.board.Children(ctx, parent)
		if kerr != nil {
			return nil, itemListOutput{}, mcpErr(kerr)
		}
		items = narrowByQuery(kids, query)
	case query != "":
		boardID := bd.ID
		if allBoards {
			boardID = ""
		}
		if items, err = h.board.SearchItems(ctx, ws.ID, boardID, query, in.IncludeArchived); err != nil {
			return nil, itemListOutput{}, mcpErr(err)
		}
		items = h.floatRefMatch(ctx, ws, query, items, in.IncludeArchived)
	case parent != "":
		if _, err := h.mcpItem(ctx, parent, ws.ID); err != nil {
			return nil, itemListOutput{}, err
		}
		if items, err = h.board.Children(ctx, parent); err != nil {
			return nil, itemListOutput{}, mcpErr(err)
		}
	default:
		all, ierr := h.board.Items(ctx, ws.ID)
		if ierr != nil {
			return nil, itemListOutput{}, mcpErr(ierr)
		}
		if allBoards {
			items = all
		} else {
			items = itemsOnBoard(all, boardStatuses)
		}
	}

	// Labels cover every board (a subtask may sit on another); the done lane for
	// progress is the chosen board's last lane.
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
	projectSlug, err := h.projectSlugs(ctx, ws.ID)
	if err != nil {
		return nil, itemListOutput{}, mcpErr(err)
	}
	// An optional project slug narrows to one project's items.
	projectID := ""
	if s := strings.TrimSpace(in.Project); s != "" {
		if projectID, err = h.projectIDBySlug(ctx, ws.ID, s); err != nil {
			return nil, itemListOutput{}, mcpErr(err)
		}
	}
	releaseIDByItem, releaseNameByItem, err := h.itemReleaseMaps(ctx, ws.ID)
	if err != nil {
		return nil, itemListOutput{}, mcpErr(err)
	}
	// An optional release name narrows to one release's items.
	releaseID := ""
	if s := strings.TrimSpace(in.Release); s != "" {
		if releaseID, err = h.releaseIDByName(ctx, ws.ID, s); err != nil {
			return nil, itemListOutput{}, mcpErr(err)
		}
	}
	// Optional attribute filters (a slug, "none" matching unset). A bad slug fails
	// before listing.
	priorityVal, filterPriority, err := resolveAttrFilter(board.Priorities, in.Priority)
	if err != nil {
		return nil, itemListOutput{}, mcpErr(err)
	}
	typeVal, filterType, err := resolveAttrFilter(board.ItemTypes, in.Type)
	if err != nil {
		return nil, itemListOutput{}, mcpErr(err)
	}
	sizeVal, filterSize, err := resolveAttrFilter(board.Sizes, in.Size)
	if err != nil {
		return nil, itemListOutput{}, mcpErr(err)
	}
	doneStatusID := ""
	if n := len(boardStatuses); n > 0 {
		doneStatusID = boardStatuses[n-1].ID
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
		if projectID != "" && it.ProjectID != projectID {
			continue
		}
		if releaseID != "" && releaseIDByItem[it.ID] != releaseID {
			continue
		}
		if filterPriority && it.Priority != priorityVal {
			continue
		}
		if filterType && it.Type != typeVal {
			continue
		}
		if filterSize && it.Size != sizeVal {
			continue
		}
		if in.Overdue && !board.Overdue(it.DueDate, it.StatusID == doneStatusID) {
			continue
		}
		v := toMCPItem(it, statusName, userName, projectSlug, ws.ItemPrefix)
		v.Release = releaseNameByItem[it.ID]
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
	statusName, userName, projectSlug, err := h.nameMaps(ctx, item.WorkspaceID)
	if err != nil {
		return nil, mcpItemDetail{}, mcpErr(err)
	}
	_, releaseName, err := h.itemReleaseMaps(ctx, item.WorkspaceID)
	if err != nil {
		return nil, mcpItemDetail{}, mcpErr(err)
	}
	slug := h.slugFor(ctx, item.WorkspaceID)
	prefix := h.prefixFor(ctx, item.WorkspaceID)
	root := toMCPItem(item, statusName, userName, projectSlug, prefix)
	root.Release = releaseName[item.ID]
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
		cv := toMCPItem(c, statusName, userName, projectSlug, prefix)
		cv.Release = releaseName[c.ID]
		cv.URL = h.itemURL(slug, c.ID)
		detail.Subtasks = append(detail.Subtasks, cv)
	}

	comments, err := h.board.Comments(ctx, item.ID)
	if err != nil {
		return nil, mcpItemDetail{}, mcpErr(err)
	}
	for _, c := range comments {
		detail.Comments = append(detail.Comments, commentAPI{
			ID:     c.ID,
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

	// Validate the attribute inputs up front so a bad slug/date fails before we
	// create anything.
	priorityVal, err := parseAttrInput(board.Priorities, in.Priority)
	if err != nil {
		return nil, mcpItem{}, mcpErr(err)
	}
	typeVal, err := parseAttrInput(board.ItemTypes, in.Type)
	if err != nil {
		return nil, mcpItem{}, mcpErr(err)
	}
	sizeVal, err := parseAttrInput(board.Sizes, in.Size)
	if err != nil {
		return nil, mcpItem{}, mcpErr(err)
	}
	due, err := board.ParseDue(in.Due)
	if err != nil {
		return nil, mcpItem{}, mcpErr(fmt.Errorf("invalid due date %q (want YYYY-MM-DD)", in.Due))
	}

	var it store.Item
	if parent := strings.ToLower(strings.TrimSpace(in.Parent)); parent != "" {
		if _, err := h.mcpItem(ctx, parent, ws.ID); err != nil {
			return nil, mcpItem{}, err
		}
		it, err = h.board.CreateSubtaskAs(ctx, parent, in.Title, p.ID)
	} else {
		bd, berr := h.mcpBoard(ctx, ws, in.Board)
		if berr != nil {
			return nil, mcpItem{}, berr
		}
		boardStatuses, serr := h.board.BoardStatuses(ctx, bd.ID)
		if serr != nil {
			return nil, mcpItem{}, mcpErr(serr)
		}
		var statusID string
		if s := strings.TrimSpace(in.Status); s != "" {
			if statusID, err = statusIDInList(boardStatuses, s); err != nil {
				return nil, mcpItem{}, mcpErr(err)
			}
		} else {
			entry, eerr := h.board.EntryStatus(ctx, bd.ID)
			if eerr != nil {
				return nil, mcpItem{}, mcpErr(eerr)
			}
			statusID = entry.ID
		}
		it, err = h.board.CreateRootItemAs(ctx, ws.ID, statusID, in.Title, p.ID)
	}
	if err != nil {
		return nil, mcpItem{}, mcpErr(err)
	}
	// Optionally file the new item under a project (subtasks otherwise inherit
	// their parent's project; an explicit project overrides that).
	if s := strings.TrimSpace(in.Project); s != "" {
		projectID, perr := h.projectIDBySlug(ctx, ws.ID, s)
		if perr != nil {
			return nil, mcpItem{}, mcpErr(perr)
		}
		if perr := h.board.SetItemProject(ctx, it.ID, projectID); perr != nil {
			return nil, mcpItem{}, mcpErr(perr)
		}
		it.ProjectID = projectID
	}
	// Optionally add the new item to a release (by name).
	if s := strings.TrimSpace(in.Release); s != "" {
		releaseID, rerr := h.releaseIDByName(ctx, ws.ID, s)
		if rerr != nil {
			return nil, mcpItem{}, mcpReleaseErr(rerr)
		}
		if rerr := h.board.SetItemRelease(ctx, it.ID, releaseID); rerr != nil {
			return nil, mcpItem{}, mcpReleaseErr(rerr)
		}
	}
	// Optional attributes (validated above; 0/nil are no-ops).
	for _, apply := range []func() error{
		func() error { return h.board.SetPriority(ctx, it.ID, priorityVal) },
		func() error { return h.board.SetType(ctx, it.ID, typeVal) },
		func() error { return h.board.SetSize(ctx, it.ID, sizeVal) },
		func() error { return h.board.SetDue(ctx, it.ID, due) },
	} {
		if err := apply(); err != nil {
			return nil, mcpItem{}, mcpErr(err)
		}
	}
	it.Priority, it.Type, it.Size, it.DueDate = priorityVal, typeVal, sizeVal, due
	if it.ParentID != "" {
		h.publishSubtaskAdd("", it.WorkspaceID, it)
	} else {
		h.publishItemUpsert(ctx, "", it.WorkspaceID, it)
	}
	return h.mcpItemResult(ctx, it)
}

// parseAttrInput resolves an optional enum slug to its value (0 = unset/no-op);
// an unknown slug is an error so a typo isn't silently ignored.
func parseAttrInput(v board.AttrVocab, raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	val, ok := v.Parse(raw)
	if !ok {
		return 0, fmt.Errorf("unknown value %q", raw)
	}
	return val, nil
}

// resolveAttrFilter is parseAttrInput for the list filters: an empty slug means
// "no filter" (the bool is false); "none" is a real filter for unset items.
func resolveAttrFilter(v board.AttrVocab, raw string) (int, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false, nil
	}
	val, ok := v.Parse(raw)
	if !ok {
		return 0, false, fmt.Errorf("unknown value %q", raw)
	}
	return val, true, nil
}

func (h *handlers) mcpListProjects(ctx context.Context, _ *mcp.CallToolRequest, in listProjectsInput) (*mcp.CallToolResult, projectListOutput, error) {
	ws, err := h.mcpWorkspace(ctx, in.Workspace)
	if err != nil {
		return nil, projectListOutput{}, err
	}
	list, err := h.listProjectsAPI(ctx, ws.ID)
	if err != nil {
		return nil, projectListOutput{}, mcpErr(err)
	}
	return &mcp.CallToolResult{}, projectListOutput{Projects: list}, nil
}

func (h *handlers) mcpCreateProject(ctx context.Context, _ *mcp.CallToolRequest, in createProjectInput) (*mcp.CallToolResult, projectAPI, error) {
	ws, err := h.mcpWorkspace(ctx, in.Workspace)
	if err != nil {
		return nil, projectAPI{}, err
	}
	p, err := h.createProjectShared(ctx, ws, in.Name, in.Brief, in.Status, in.Lead)
	if err != nil {
		return nil, projectAPI{}, mcpProjectErr(err)
	}
	return &mcp.CallToolResult{}, h.projectAPIFor(ctx, p, store.SubtaskCount{}), nil
}

func (h *handlers) mcpSetItemProject(ctx context.Context, _ *mcp.CallToolRequest, in setItemProjectInput) (*mcp.CallToolResult, mcpItem, error) {
	item, err := h.mcpItem(ctx, in.ID, "")
	if err != nil {
		return nil, mcpItem{}, err
	}
	projectID := ""
	if s := strings.TrimSpace(in.Project); s != "" {
		if projectID, err = h.projectIDBySlug(ctx, item.WorkspaceID, s); err != nil {
			return nil, mcpItem{}, mcpProjectErr(err)
		}
	}
	if err := h.board.SetItemProject(ctx, item.ID, projectID); err != nil {
		return nil, mcpItem{}, mcpProjectErr(err)
	}
	h.liveUpsertOrigin(ctx, "", item.ID)
	return h.mcpReloadResult(ctx, item.ID)
}

// mcpProjectErr adds the project-specific sentinels to mcpErr's mapping (the
// name-lookup errors are already human-readable and pass through).
func mcpProjectErr(err error) error {
	switch {
	case errors.Is(err, errUnknownProject):
		return err
	case errors.Is(err, board.ErrInvalidProjectName):
		return errors.New("invalid project name (1–80 characters)")
	case errors.Is(err, board.ErrInvalidProjectStatus):
		return errors.New("invalid status (use planned/active/paused/done)")
	case errors.Is(err, board.ErrInvalidProjectBrief):
		return errors.New("brief too long")
	case errors.Is(err, board.ErrProjectMismatch):
		return errors.New("project belongs to another workspace")
	default:
		return mcpErr(err)
	}
}

func (h *handlers) mcpListReleases(ctx context.Context, _ *mcp.CallToolRequest, in listReleasesInput) (*mcp.CallToolResult, releaseListOutput, error) {
	ws, err := h.mcpWorkspace(ctx, in.Workspace)
	if err != nil {
		return nil, releaseListOutput{}, err
	}
	list, err := h.listReleasesAPI(ctx, ws.ID)
	if err != nil {
		return nil, releaseListOutput{}, mcpErr(err)
	}
	return &mcp.CallToolResult{}, releaseListOutput{Releases: list}, nil
}

func (h *handlers) mcpCreateRelease(ctx context.Context, _ *mcp.CallToolRequest, in createReleaseInput) (*mcp.CallToolResult, releaseAPI, error) {
	ws, err := h.mcpWorkspace(ctx, in.Workspace)
	if err != nil {
		return nil, releaseAPI{}, err
	}
	rel, err := h.createReleaseShared(ctx, ws, in.Name, in.Description, in.Status)
	if err != nil {
		return nil, releaseAPI{}, mcpReleaseErr(err)
	}
	return &mcp.CallToolResult{}, toReleaseAPI(rel, store.SubtaskCount{}), nil
}

func (h *handlers) mcpSetItemRelease(ctx context.Context, _ *mcp.CallToolRequest, in setItemReleaseInput) (*mcp.CallToolResult, mcpItem, error) {
	item, err := h.mcpItem(ctx, in.ID, "")
	if err != nil {
		return nil, mcpItem{}, err
	}
	releaseID := ""
	if s := strings.TrimSpace(in.Release); s != "" {
		if releaseID, err = h.releaseIDByName(ctx, item.WorkspaceID, s); err != nil {
			return nil, mcpItem{}, mcpReleaseErr(err)
		}
	}
	if err := h.board.SetItemRelease(ctx, item.ID, releaseID); err != nil {
		return nil, mcpItem{}, mcpReleaseErr(err)
	}
	h.liveUpsertOrigin(ctx, "", item.ID)
	return h.mcpReloadResult(ctx, item.ID)
}

func (h *handlers) mcpSetReleaseStatus(ctx context.Context, _ *mcp.CallToolRequest, in setReleaseStatusInput) (*mcp.CallToolResult, releaseAPI, error) {
	ws, err := h.mcpWorkspace(ctx, in.Workspace)
	if err != nil {
		return nil, releaseAPI{}, err
	}
	id, err := h.releaseIDByName(ctx, ws.ID, in.Release)
	if err != nil {
		return nil, releaseAPI{}, mcpReleaseErr(err)
	}
	if err := h.board.SetReleaseStatus(ctx, id, strings.TrimSpace(in.Status)); err != nil {
		return nil, releaseAPI{}, mcpReleaseErr(err)
	}
	rel, err := h.board.Release(ctx, id)
	if err != nil {
		return nil, releaseAPI{}, mcpErr(err)
	}
	return &mcp.CallToolResult{}, toReleaseAPI(rel, store.SubtaskCount{}), nil
}

// mcpReleaseErr adds the release-specific sentinels to mcpErr's mapping (the
// name-lookup errors are already human-readable and pass through).
func mcpReleaseErr(err error) error {
	switch {
	case errors.Is(err, errUnknownRelease):
		return err
	case errors.Is(err, board.ErrInvalidReleaseName):
		return errors.New("invalid release name (1–80 characters)")
	case errors.Is(err, board.ErrInvalidReleaseStatus):
		return errors.New("invalid status (use planned/active/shipped; create accepts only planned/active)")
	case errors.Is(err, board.ErrInvalidReleaseDesc):
		return errors.New("release description too long")
	case errors.Is(err, board.ErrReleaseMismatch):
		return errors.New("release belongs to another workspace")
	case errors.Is(err, store.ErrReleaseNameTaken):
		return errors.New("a release with that name already exists")
	default:
		return mcpErr(err)
	}
}

func (h *handlers) mcpListSubscriptions(ctx context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, subscriptionListOutput, error) {
	p := principalFrom(ctx)
	if p == nil {
		return nil, subscriptionListOutput{}, errors.New("not authenticated")
	}
	subs, err := h.board.Subscriptions(ctx, p.ID, "")
	if err != nil {
		return nil, subscriptionListOutput{}, mcpErr(err)
	}
	out := subscriptionListOutput{Subscriptions: make([]subscriptionAPI, 0, len(subs))}
	for _, s := range subs {
		out.Subscriptions = append(out.Subscriptions, h.toSubscriptionAPI(ctx, s))
	}
	return &mcp.CallToolResult{}, out, nil
}

func (h *handlers) mcpSubscribe(ctx context.Context, _ *mcp.CallToolRequest, in subscribeInput) (*mcp.CallToolResult, subscriptionAPI, error) {
	p := principalFrom(ctx)
	if p == nil {
		return nil, subscriptionAPI{}, errors.New("not authenticated")
	}
	if !validSubjectType(in.Type) {
		return nil, subscriptionAPI{}, errUnknownSubjectType
	}
	id, err := h.resolveSubjectRef(ctx, in.Type, in.Ref, in.Workspace)
	if err != nil {
		return nil, subscriptionAPI{}, mcpErr(err)
	}
	var sub store.Subscription
	if len(in.Events) > 0 {
		sub, err = h.board.SetSubscription(ctx, p.ID, in.Type, id, in.Events)
	} else {
		sub, err = h.board.Subscribe(ctx, p.ID, in.Type, id)
	}
	if err != nil {
		return nil, subscriptionAPI{}, mcpErr(err)
	}
	return &mcp.CallToolResult{}, h.toSubscriptionAPI(ctx, sub), nil
}

func (h *handlers) mcpUnsubscribe(ctx context.Context, _ *mcp.CallToolRequest, in unsubscribeInput) (*mcp.CallToolResult, unsubscribeOutput, error) {
	p := principalFrom(ctx)
	if p == nil {
		return nil, unsubscribeOutput{}, errors.New("not authenticated")
	}
	if !validSubjectType(in.Type) {
		return nil, unsubscribeOutput{}, errUnknownSubjectType
	}
	id, err := h.resolveSubjectRef(ctx, in.Type, in.Ref, in.Workspace)
	if err != nil {
		return nil, unsubscribeOutput{}, mcpErr(err)
	}
	if err := h.board.Unsubscribe(ctx, p.ID, in.Type, id); err != nil {
		return nil, unsubscribeOutput{}, mcpErr(err)
	}
	return &mcp.CallToolResult{}, unsubscribeOutput{OK: true}, nil
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
	if _, err := h.board.ConfirmStatus(ctx, item.ID, statusID, in.Checklist); err != nil {
		return nil, mcpItem{}, mcpErr(err)
	}
	h.liveUpsertOrigin(ctx, "", item.ID)
	return h.mcpReloadResult(ctx, item.ID)
}

func (h *handlers) mcpClaimItem(ctx context.Context, _ *mcp.CallToolRequest, in claimItemInput) (*mcp.CallToolResult, mcpItem, error) {
	item, err := h.mcpItem(ctx, in.ID, "")
	if err != nil {
		return nil, mcpItem{}, err
	}
	p := principalFrom(ctx)
	if p == nil {
		return nil, mcpItem{}, errors.New("not authenticated")
	}
	comment := strings.TrimSpace(in.Comment)
	if comment != "" && len([]rune(comment)) > board.MaxCommentLen {
		return nil, mcpItem{}, mcpErr(board.ErrInvalidComment)
	}
	if status := strings.TrimSpace(in.Status); status != "" {
		statusID, err := h.statusIDByName(ctx, item.WorkspaceID, status)
		if err != nil {
			return nil, mcpItem{}, mcpErr(err)
		}
		if _, err := h.board.ConfirmStatus(ctx, item.ID, statusID, in.Checklist); err != nil {
			return nil, mcpItem{}, mcpErr(err)
		}
	}
	if err := h.board.SetAssignee(ctx, item.ID, p.ID); err != nil {
		return nil, mcpItem{}, mcpErr(err)
	}
	if comment != "" {
		if _, _, err := h.addMCPComment(ctx, item.ID, comment); err != nil {
			return nil, mcpItem{}, err
		}
	}
	h.liveUpsertOrigin(ctx, "", item.ID)
	return h.mcpReloadResult(ctx, item.ID)
}

func (h *handlers) mcpSetItemTitle(ctx context.Context, _ *mcp.CallToolRequest, in setItemTitleInput) (*mcp.CallToolResult, mcpItem, error) {
	item, err := h.mcpItem(ctx, in.ID, "")
	if err != nil {
		return nil, mcpItem{}, err
	}
	if err := h.board.RenameItem(ctx, item.ID, in.Title); err != nil {
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

func (h *handlers) mcpSetItemPriority(ctx context.Context, _ *mcp.CallToolRequest, in setItemPriorityInput) (*mcp.CallToolResult, mcpItem, error) {
	return h.mcpSetItemEnum(ctx, in.ID, board.Priorities, in.Priority, h.board.SetPriority)
}

func (h *handlers) mcpSetItemType(ctx context.Context, _ *mcp.CallToolRequest, in setItemTypeInput) (*mcp.CallToolResult, mcpItem, error) {
	return h.mcpSetItemEnum(ctx, in.ID, board.ItemTypes, in.Type, h.board.SetType)
}

func (h *handlers) mcpSetItemSize(ctx context.Context, _ *mcp.CallToolRequest, in setItemSizeInput) (*mcp.CallToolResult, mcpItem, error) {
	return h.mcpSetItemEnum(ctx, in.ID, board.Sizes, in.Size, h.board.SetSize)
}

// mcpSetItemEnum is the shared body of the priority/type/size setters: resolve the
// item, parse the slug (empty clears), apply, and return the reloaded item.
func (h *handlers) mcpSetItemEnum(ctx context.Context, id string, vocab board.AttrVocab, raw string, set func(context.Context, string, int) error) (*mcp.CallToolResult, mcpItem, error) {
	item, err := h.mcpItem(ctx, id, "")
	if err != nil {
		return nil, mcpItem{}, err
	}
	val, err := parseAttrInput(vocab, raw)
	if err != nil {
		return nil, mcpItem{}, mcpErr(err)
	}
	if err := set(ctx, item.ID, val); err != nil {
		return nil, mcpItem{}, mcpErr(err)
	}
	h.liveUpsertOrigin(ctx, "", item.ID)
	return h.mcpReloadResult(ctx, item.ID)
}

func (h *handlers) mcpSetItemDue(ctx context.Context, _ *mcp.CallToolRequest, in setItemDueInput) (*mcp.CallToolResult, mcpItem, error) {
	item, err := h.mcpItem(ctx, in.ID, "")
	if err != nil {
		return nil, mcpItem{}, err
	}
	due, err := board.ParseDue(in.Due)
	if err != nil {
		return nil, mcpItem{}, mcpErr(fmt.Errorf("invalid due date %q (want YYYY-MM-DD)", in.Due))
	}
	if err := h.board.SetDue(ctx, item.ID, due); err != nil {
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
	return h.addMCPComment(ctx, in.ID, in.Body)
}

func (h *handlers) addMCPComment(ctx context.Context, id, body string) (*mcp.CallToolResult, commentAPI, error) {
	item, err := h.mcpItem(ctx, id, "")
	if err != nil {
		return nil, commentAPI{}, err
	}
	p := principalFrom(ctx)
	c, notified, err := h.board.AddComment(ctx, item.ID, p.ID, body)
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
		ID:     c.ID,
		Author: p.Username,
		Body:   c.Body,
		At:     c.CreatedAt.Format(time.RFC3339),
	}, nil
}

// --- documents ---

type documentAPI struct {
	ID        string `json:"id"`
	Item      string `json:"item,omitempty"`
	Title     string `json:"title"`
	Body      string `json:"body,omitempty"` // omitted in list summaries; present on get/create/update
	Author    string `json:"author,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

type listDocumentsInput struct {
	Item string `json:"item" jsonschema:"the item id whose documents to list"`
}

type listDocumentsOutput struct {
	Documents []documentAPI `json:"documents"`
}

type documentRefInput struct {
	ID string `json:"id" jsonschema:"the document id (from list_documents)"`
}

type createDocumentInput struct {
	Item  string `json:"item" jsonschema:"the item id to attach the document to"`
	Title string `json:"title" jsonschema:"the document title"`
	Body  string `json:"body" jsonschema:"the document body (markdown)"`
}

type updateDocumentInput struct {
	ID    string `json:"id" jsonschema:"the document id (from list_documents)"`
	Title string `json:"title" jsonschema:"the new title"`
	Body  string `json:"body" jsonschema:"the new body (markdown); replaces the old one"`
}

// documentAPIFull renders a document with its body and resolved author.
func (h *handlers) documentAPIFull(ctx context.Context, d store.Document) documentAPI {
	return documentAPI{
		ID:        d.ID,
		Item:      d.ItemID,
		Title:     d.Title,
		Body:      d.Body,
		Author:    h.authorName(ctx, d.AuthorID),
		CreatedAt: d.CreatedAt.Format(time.RFC3339),
		UpdatedAt: d.UpdatedAt.Format(time.RFC3339),
	}
}

func (h *handlers) mcpListDocuments(ctx context.Context, _ *mcp.CallToolRequest, in listDocumentsInput) (*mcp.CallToolResult, listDocumentsOutput, error) {
	item, err := h.mcpItem(ctx, in.Item, "")
	if err != nil {
		return nil, listDocumentsOutput{}, err
	}
	docs, err := h.board.Documents(ctx, item.ID)
	if err != nil {
		return nil, listDocumentsOutput{}, mcpErr(err)
	}
	out := listDocumentsOutput{Documents: make([]documentAPI, 0, len(docs))}
	for _, d := range docs {
		// Summary: omit the body to keep the listing light (bodies can be large).
		out.Documents = append(out.Documents, documentAPI{
			ID:        d.ID,
			Title:     d.Title,
			Author:    h.authorName(ctx, d.AuthorID),
			CreatedAt: d.CreatedAt.Format(time.RFC3339),
			UpdatedAt: d.UpdatedAt.Format(time.RFC3339),
		})
	}
	return &mcp.CallToolResult{}, out, nil
}

func (h *handlers) mcpGetDocument(ctx context.Context, _ *mcp.CallToolRequest, in documentRefInput) (*mcp.CallToolResult, documentAPI, error) {
	d, err := h.board.Document(ctx, in.ID)
	if err != nil {
		return nil, documentAPI{}, mcpErr(err)
	}
	if _, err := h.mcpItem(ctx, d.ItemID, ""); err != nil { // workspace-access guard
		return nil, documentAPI{}, err
	}
	return &mcp.CallToolResult{}, h.documentAPIFull(ctx, d), nil
}

func (h *handlers) mcpCreateDocument(ctx context.Context, _ *mcp.CallToolRequest, in createDocumentInput) (*mcp.CallToolResult, documentAPI, error) {
	item, err := h.mcpItem(ctx, in.Item, "")
	if err != nil {
		return nil, documentAPI{}, err
	}
	p := principalFrom(ctx)
	d, err := h.board.AddDocument(ctx, item.ID, p.ID, in.Title, in.Body)
	if err != nil {
		return nil, documentAPI{}, mcpErr(err)
	}
	dv := documentToView(d, p.Display)
	h.publishLive(wsTopic(item.WorkspaceID), "document.add", "", map[string]any{
		"item": item.ID, "id": d.ID, "html": docCardHTML(dv),
	})
	return &mcp.CallToolResult{}, h.documentAPIFull(ctx, d), nil
}

func (h *handlers) mcpUpdateDocument(ctx context.Context, _ *mcp.CallToolRequest, in updateDocumentInput) (*mcp.CallToolResult, documentAPI, error) {
	cur, err := h.board.Document(ctx, in.ID)
	if err != nil {
		return nil, documentAPI{}, mcpErr(err)
	}
	if _, err := h.mcpItem(ctx, cur.ItemID, ""); err != nil { // workspace-access guard
		return nil, documentAPI{}, err
	}
	d, err := h.board.EditDocument(ctx, in.ID, in.Title, in.Body)
	if err != nil {
		return nil, documentAPI{}, mcpErr(err)
	}
	dv := documentToView(d, h.authorName(ctx, d.AuthorID))
	if item, ierr := h.board.Item(ctx, d.ItemID); ierr == nil {
		h.publishLive(wsTopic(item.WorkspaceID), "document.edit", "", map[string]any{
			"item": d.ItemID, "id": d.ID, "html": docCardHTML(dv),
		})
	}
	return &mcp.CallToolResult{}, h.documentAPIFull(ctx, d), nil
}

func (h *handlers) mcpDeleteDocument(ctx context.Context, _ *mcp.CallToolRequest, in documentRefInput) (*mcp.CallToolResult, documentAPI, error) {
	cur, err := h.board.Document(ctx, in.ID)
	if err != nil {
		return nil, documentAPI{}, mcpErr(err)
	}
	if _, err := h.mcpItem(ctx, cur.ItemID, ""); err != nil { // workspace-access guard
		return nil, documentAPI{}, err
	}
	d, err := h.board.RemoveDocument(ctx, in.ID)
	if err != nil {
		return nil, documentAPI{}, mcpErr(err)
	}
	if item, ierr := h.board.Item(ctx, d.ItemID); ierr == nil {
		h.publishLive(wsTopic(item.WorkspaceID), "document.delete", "", map[string]any{
			"item": d.ItemID, "id": d.ID,
		})
	}
	return &mcp.CallToolResult{}, documentAPI{ID: d.ID, Title: d.Title}, nil
}

// mcpWatchComments is the "listen to a task for comments" primitive: a bounded,
// cursor-based blocking read of an item's comment stream. It returns the
// comments after the cursor as soon as any exist, otherwise it parks on the live
// broker until one is posted or the (CF-safe) window lapses, returning empty.
func (h *handlers) mcpWatchComments(ctx context.Context, _ *mcp.CallToolRequest, in watchCommentsInput) (*mcp.CallToolResult, watchCommentsOutput, error) {
	item, err := h.mcpItem(ctx, in.Item, "")
	if err != nil {
		return nil, watchCommentsOutput{}, err
	}
	_, userName, _, err := h.nameMaps(ctx, item.WorkspaceID)
	if err != nil {
		return nil, watchCommentsOutput{}, mcpErr(err)
	}

	// An omitted cursor anchors to the item's current latest comment, so the
	// watch yields only comments posted from this call onward.
	cursorID := strings.TrimSpace(in.After)
	if cursorID == "" {
		cs, err := h.board.Comments(ctx, item.ID)
		if err != nil {
			return nil, watchCommentsOutput{}, mcpErr(err)
		}
		if n := len(cs); n > 0 {
			cursorID = cs[n-1].ID
		}
	}

	wctx, cancel := context.WithTimeout(ctx, clampWatchTimeout(in.TimeoutSeconds))
	defer cancel()

	// Subscribe before the first read so a comment landing in the gap still wakes
	// us (the lost-wakeup guard, same as the SSE handler).
	var ch <-chan []byte
	if h.live != nil {
		ch = h.live.Subscribe(wctx, wsTopic(item.WorkspaceID))
	}

	for {
		out, newCursor, err := h.commentsAfterCursor(ctx, item.ID, cursorID, userName)
		if err != nil {
			return nil, watchCommentsOutput{}, mcpErr(err)
		}
		// Deliver as soon as there's anything; with no broker there's nothing to
		// block on, so a single read is the whole answer.
		if len(out) > 0 || ch == nil {
			return &mcp.CallToolResult{}, watchCommentsOutput{Comments: out, Cursor: newCursor}, nil
		}
		if !waitForItemComment(wctx, ch, item.ID) {
			return &mcp.CallToolResult{}, watchCommentsOutput{Comments: []commentAPI{}, Cursor: cursorID}, nil
		}
	}
}

// commentsAfterCursor returns the item's comments after the comment with id
// cursorID (all of them when cursorID is empty), plus the new cursor (the newest
// returned id, or the unchanged cursorID when none follow). An unknown cursor is
// an error rather than a silent full replay.
func (h *handlers) commentsAfterCursor(ctx context.Context, itemID, cursorID string, userName map[string]string) ([]commentAPI, string, error) {
	cs, err := h.board.Comments(ctx, itemID)
	if err != nil {
		return nil, cursorID, err
	}
	start := 0
	if cursorID != "" {
		idx := -1
		for i, c := range cs {
			if c.ID == cursorID {
				idx = i + 1
				break
			}
		}
		if idx < 0 {
			return nil, cursorID, errors.New("watch_comments: unknown after cursor")
		}
		start = idx
	}
	out := make([]commentAPI, 0, len(cs)-start)
	for _, c := range cs[start:] {
		out = append(out, commentAPI{
			ID:     c.ID,
			Author: userName[c.AuthorID],
			Body:   c.Body,
			At:     c.CreatedAt.Format(time.RFC3339),
		})
	}
	cursor := cursorID
	if n := len(out); n > 0 {
		cursor = out[n-1].ID
	}
	return out, cursor, nil
}

// waitForItemComment blocks until a comment.add event for itemID arrives on ch,
// returning true, or the context is done (timeout), returning false. Events for
// other items or of other kinds are ignored.
func waitForItemComment(ctx context.Context, ch <-chan []byte, itemID string) bool {
	for {
		select {
		case <-ctx.Done():
			return false
		case data := <-ch:
			var ev struct {
				Kind string `json:"kind"`
				Item string `json:"item"`
			}
			if json.Unmarshal(data, &ev) == nil && ev.Kind == "comment.add" && ev.Item == itemID {
				return true
			}
		}
	}
}

// clampWatchTimeout bounds the blocking window: default 25s, hard cap 30s, so a
// single request always returns well under Cloudflare's ~100s idle limit.
func clampWatchTimeout(sec int) time.Duration {
	switch {
	case sec <= 0:
		return 25 * time.Second
	case sec > 30:
		return 30 * time.Second
	default:
		return time.Duration(sec) * time.Second
	}
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
	switch {
	case strings.TrimSpace(in.Item) != "":
		// Scope to one item, but keep it inside the named workspace.
		it, ierr := h.mcpItem(ctx, strings.TrimSpace(in.Item), ws.ID)
		if ierr != nil {
			return nil, activityOutput{}, ierr
		}
		events, err = h.board.ItemHistory(ctx, it.ID, limit)
	case strings.TrimSpace(in.Board) != "":
		bd, berr := h.mcpBoard(ctx, ws, in.Board)
		if berr != nil {
			return nil, activityOutput{}, berr
		}
		events, err = h.board.BoardActivity(ctx, bd.ID, limit)
	default:
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
			Verb:      n.Verb,
			Summary:   n.Summary,
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
	statusName, userName, projectSlug, err := h.nameMaps(ctx, it.WorkspaceID)
	if err != nil {
		return nil, mcpItem{}, mcpErr(err)
	}
	v := toMCPItem(it, statusName, userName, projectSlug, h.prefixFor(ctx, it.WorkspaceID))
	v.Release = h.releaseNameFor(ctx, it.ID)
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
// isChecklistErr reports whether err is a checklist-gate failure whose message
// is safe (and useful) to surface verbatim to the agent.
func isChecklistErr(err error) bool {
	var ce *board.ChecklistError
	var ue *board.UnknownFactError
	return errors.As(err, &ce) || errors.As(err, &ue)
}

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
	case errors.Is(err, errUnknownStatus), errors.Is(err, errUnknownUser),
		errors.Is(err, errUnknownProject), errors.Is(err, errUnknownSubjectType),
		errors.Is(err, errProjectNeedsWS):
		return err
	case isChecklistErr(err):
		// The checklist messages name the lane and the still-required (or
		// unknown) facts — pass them straight through for the agent to act on.
		return err
	default:
		slog.Error("mcp tool error", "err", err)
		return fmt.Errorf("internal error")
	}
}
