package migration

// Also exposed to agents; keep accepted fields and semantics here.
var Fields = map[string][]string{
	"workspace": {"name", "slug", "description", "prefix", "status_board", "statuses", "creation_status", "completed_status", "created_at"},
	"task":      {"workspace_id", "reference", "title", "description", "priority", "type", "size", "status_id", "parent_id", "assignees", "archived_at", "created_by", "created_at", "updated_at"},
	"comment":   {"task_id", "body", "reply_to", "author_id", "created_at", "updated_at"},
	"memory":    {"scope", "scope_id", "key", "summary", "content", "created_by", "created_at", "updated_at"},
	"document":  {"task_id", "title", "filename", "content_base64", "created_at"},
	"activity":  {"task_id", "change", "created_at", "updated_at"},
}

const Instructions = `Migration Assistant is superuser-delegated content authority, independent of any source system. Writes require acting_as: an existing human or agent UUID (historical authors may be disabled/pending). Discover accounts using migration_find. Inspect before editing and supply the opaque revision returned. UUIDs are generated. Create is not idempotent: inspect before retrying an uncertain response. Writes are atomic and suppress notifications; the actual operator/session/time and before/after data are retained separately.
Omitted fields remain unchanged. Null clears only task parent_id/archived_at or a new comment reply_to. Required values cannot be null. Dates are RFC3339; updated_at cannot precede created_at. Defaults: creation time now for new records, preserved when editing; update time now.
Workspace: name/slug required, description optional. prefix uses 2-10 letters. status_board selects tasks (default) or backlog for workflow edits. statuses is an array of distinct names within that board; creation_status/completed_status select different names. Backlog permits one status and an empty completed_status. Set statuses before importing tasks; used statuses cannot be removed. Existing names retain IDs.
Task: workspace_id required on create and immutable. reference accepts current PREFIX-positive_number (PEI-123), enforces uniqueness and advances allocation. Parent must be in the same workspace; cycles fail. Assignees are distinct existing account UUIDs, including historical assignments. Status must belong to the workspace; its board determines task board membership. priority/type/size use normal Acta values. created_by defaults to acting_as on creation and is preserved on edit unless specified.
Comment: task_id/body required; optional reply_to on creation. task_id/reply_to immutable afterward. author_id defaults to acting_as on creation, preserved on edit unless specified. Deleted comments cannot be rewritten.
Memory: scope is site/workspace/user/agent; scope_id required except site and must identify the corresponding workspace or account kind. Scope is immutable after creation. key/summary/content use standard validation. created_by has task semantics; updated_by is acting_as.
Document: task_id/filename/content_base64 required on create. Edit appends an immutable version, reusing omitted title/filename/content. task_id immutable; created_at dates the new version. Files are limited to 20 MiB decoded, with a 28 MiB MCP request limit for migration connections.
Activity: task_id/change required; task_id immutable. change is an Acta Change object (kind,field,reason,before/after text/items). Only recognized task/document change shapes are accepted. Edits append a correction event; original events remain immutable. Dates control displayed timestamps; pagination remains insertion ordered, so create historical entries chronologically.
These tools cannot write accounts, credentials, memberships, sessions, security settings, permission grants or superuser access. Delegation stops working when its human owner loses superuser authority.`
