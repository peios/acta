package memories

import (
	"acta/internal/accounts"
	"errors"
	"testing"
)

func TestSaveScopeShapeValidation(t *testing.T) {
	empty, workspace := "", "project"
	for _, test := range []struct {
		scope        string
		workspace    *string
		agent, field string
	}{
		{"unknown", nil, "", "scope"},
		{"agent", &workspace, "", "workspace"},
		{"site", &workspace, "", "workspace"},
		{"user", &empty, "", "workspace"},
		{"workspace", nil, "", "workspace"},
		{"workspace", &empty, "", "workspace"},
		{"workspace", &workspace, "other-agent", "agent_id"},
	} {
		in := Save{Scope: test.scope, Workspace: test.workspace, AgentID: test.agent, Key: "test", Summary: "Summary", Content: "Content"}
		var field *accounts.FieldError
		if e := Validate(&in); !errors.As(e, &field) || field.Field != test.field {
			t.Fatalf("%+v: %v", test, e)
		}
	}
	for _, scope := range []string{"agent", "site", "user", "workspace"} {
		in := Save{Scope: scope, Key: "test", Summary: "Summary", Content: "Content"}
		if scope == "workspace" {
			in.Workspace = &workspace
		}
		if e := Validate(&in); e != nil {
			t.Fatal(scope, e)
		}
	}
}
