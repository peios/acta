package httpapi

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"acta2/internal/tasks"
)

func TestUnknownErrorsAreSanitized(t *testing.T) {
	status, p := classifyError(errors.New("database password=private table=secret"))
	if status != 503 || p.Code != "unavailable" || strings.Contains(p.Message, "private") || p.Current != nil {
		t.Fatal(status, p)
	}
}

func TestConflictPreservesTypedCurrentValue(t *testing.T) {
	for _, raw := range []string{`"Their description"`, `["account-id"]`} {
		var value any
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			t.Fatal(err)
		}
		status, p := classifyError(&tasks.Conflict{Field: "description", Version: 9007199254740993, Value: value})
		data, err := json.Marshal(p)
		if err != nil || status != 409 || p.Code != "conflict" || !strings.Contains(string(data), `9007199254740993`) || !strings.Contains(string(data), raw) {
			t.Fatal(string(data), err)
		}
	}
}
