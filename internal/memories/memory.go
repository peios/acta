package memories

import (
	"acta2/internal/accounts"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

var ErrConflict = errors.New("This memory changed since you read it. Reload its latest revision before saving or deleting.")

type Memory struct {
	ID        string    `json:"id"`
	Scope     string    `json:"scope"`
	ScopeID   string    `json:"scope_id"`
	Key       string    `json:"key"`
	Summary   string    `json:"summary"`
	Content   string    `json:"content,omitempty"`
	Revision  int64     `json:"revision"`
	CreatedBy string    `json:"created_by"`
	UpdatedBy string    `json:"updated_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	CanWrite  bool      `json:"can_write"`
}
type Save struct {
	ID        string  `json:"id,omitempty"`
	Scope     string  `json:"scope"`
	Workspace *string `json:"workspace,omitempty"`
	AgentID   string  `json:"agent_id,omitempty"`
	Key       string  `json:"key"`
	Summary   string  `json:"summary"`
	Content   string  `json:"content"`
	Revision  int64   `json:"revision"`
}
type Recall struct {
	Workspace *string `json:"workspace"`
	Query     string  `json:"query,omitempty"`
	Cursor    string  `json:"cursor,omitempty"`
	AgentID   string  `json:"agent_id,omitempty"`
}
type Page struct {
	Memories []Memory `json:"memories"`
	Cursor   string   `json:"cursor,omitempty"`
}

var keyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,99}$`)

func Validate(s *Save) error {
	switch s.Scope {
	case "site", "user", "agent":
		if s.Workspace != nil {
			return &accounts.FieldError{Field: "workspace", Message: "Supply workspace only for workspace-scoped memories; otherwise omit it or use null."}
		}
	case "workspace":
		if s.Workspace == nil || strings.TrimSpace(*s.Workspace) == "" {
			return &accounts.FieldError{Field: "workspace", Message: "Supply a workspace UUID or slug for workspace-scoped memories."}
		}
	default:
		return &accounts.FieldError{Field: "scope", Message: "Choose site, workspace, user or agent."}
	}
	if s.AgentID != "" && s.Scope != "agent" {
		return &accounts.FieldError{Field: "agent_id", Message: "Supply agent_id only for agent-scoped memories."}
	}

	s.Key = strings.TrimSpace(s.Key)
	s.Summary = strings.TrimSpace(s.Summary)
	for _, v := range []struct {
		field, value string
		max          int
	}{{"summary", s.Summary, 500}, {"content", s.Content, 64000}} {
		if !utf8.ValidString(v.value) || strings.ContainsRune(v.value, 0) || strings.TrimSpace(v.value) == "" || len(v.value) > v.max {
			return &accounts.FieldError{Field: v.field, Message: "Provide non-empty text within the size limit (500 bytes for summary, 64 KB for content)."}
		}
	}
	if !keyPattern.MatchString(s.Key) {
		return &accounts.FieldError{Field: "key", Message: "Use 1–100 lowercase letters, numbers or hyphens, starting with a letter or number."}
	}
	if (s.ID == "" && s.Revision != 0) || (s.ID != "" && s.Revision < 1) {
		return &accounts.FieldError{Field: "revision", Message: "Use revision 0 to create; supply the revision read when updating."}
	}
	return nil
}

const SaveDescription = `Save durable, standalone knowledge. Explicitly choose the correct scope: workspace is the normal destination for project conventions, shared knowledge and project-specific gotchas. User scope is ONLY for knowledge truly specific to this person, such as preferences across projects. Agent scope is rare: only knowledge specific to this agent's identity or role; learning something yourself does not make it agent-scoped. Site scope is rare: only knowledge truly global across this entire Acta installation, independently of any particular user or workspace. Decisions, progress and findings tied to a task belong in its description or comments. Avoid facts easily rediscovered from current source or documentation. Never fall back to another scope when permission is denied. Supply workspace only for workspace scope. User and agent identities come from authentication. Agents can read their human owner's user memories; writing them requires an explicit Write user memories (own.memories.write) account permission, in addition to this connection's memories.write tool grant. Site writes require both site.memories.read and site.memories.write account permissions. Create with revision=0 and no id; update using id and the revision from memory_get. Updates cannot move a memory to a different scope. Keys are unique within each scope.`
