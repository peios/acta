// Package migration defines explicitly delegated, source-independent content tools.
package migration

import (
	"acta2/internal/accounts"
	"encoding/json"
	"errors"
)

type Request struct {
	Kind     string                     `json:"kind" jsonschema:"workspace, task, comment, memory, document or activity. Accounts and security settings are not writable."`
	ID       string                     `json:"id,omitempty" jsonschema:"Existing Acta UUID (or task reference) for get/edit. Omit for create; UUIDs are generated."`
	ActingAs string                     `json:"acting_as,omitempty" jsonschema:"Required for writes: existing human or agent UUID used for attribution. Discover through migration_find kind=account."`
	Revision string                     `json:"revision,omitempty" jsonschema:"Required for edit: opaque revision from migration_get or a prior write."`
	Fields   map[string]json.RawMessage `json:"fields,omitempty" jsonschema:"Field values documented by migration_schema. Omitted fields stay unchanged; null clears only nullable fields. Unknown fields are rejected."`
}
type Record struct {
	Kind     string          `json:"kind"`
	ID       string          `json:"id"`
	Revision string          `json:"revision"`
	Data     json.RawMessage `json:"data"`
}
type Find struct {
	Kind   string `json:"kind" jsonschema:"account, workspace, task, comment, memory, document or activity"`
	Parent string `json:"parent,omitempty" jsonschema:"Required workspace UUID for tasks; task UUID for comments/documents/activity. Optional scope_id for memories."`
	Query  string `json:"query,omitempty"`
	Offset int    `json:"offset,omitempty"`
}
type Page struct {
	Records    []Record `json:"records"`
	More       bool     `json:"more"`
	NextOffset int      `json:"next_offset"`
}

func Invalid(field, message string) error {
	return &accounts.FieldError{Field: field, Message: message}
}

var ErrConflict = errors.New("This record changed. Inspect it again before editing.")
