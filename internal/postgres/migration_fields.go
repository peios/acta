package postgres

import (
	"acta2/internal/migration"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"
)

type migrationFields map[string]json.RawMessage

func (f migrationFields) text(k, def string) (string, error) {
	v, ok := f[k]
	if !ok {
		return def, nil
	}
	var s string
	if bytes.Equal(bytes.TrimSpace(v), []byte("null")) || json.Unmarshal(v, &s) != nil {
		return "", migration.Invalid(k, "Supply a string.")
	}
	return s, nil
}
func (f migrationFields) nullable(k, def string) (string, error) {
	if bytes.Equal(bytes.TrimSpace(f[k]), []byte("null")) {
		return "", nil
	}
	return f.text(k, def)
}
func (f migrationFields) stamp(k string, def time.Time) (time.Time, error) {
	s, e := f.text(k, def.Format(time.RFC3339Nano))
	if e != nil {
		return def, e
	}
	v, e := time.Parse(time.RFC3339Nano, s)
	if e != nil || v.Year() < 1 || v.Year() > 9999 {
		return def, migration.Invalid(k, "Supply a valid RFC3339 timestamp.")
	}
	return v.UTC(), nil
}
func (f migrationFields) dates(created, now time.Time) (time.Time, time.Time, error) {
	a, e := f.stamp("created_at", created)
	if e != nil {
		return a, now, e
	}
	b, e := f.stamp("updated_at", now)
	if e == nil && b.Before(a) {
		e = migration.Invalid("updated_at", "Updated time must not precede creation.")
	}
	return a, b, e
}
func (f migrationFields) list(k string, def []string) ([]string, error) {
	raw, ok := f[k]
	if !ok {
		return def, nil
	}
	var v []string
	if string(raw) == "null" || json.Unmarshal(raw, &v) != nil {
		return nil, migration.Invalid(k, "Supply an array of strings; [] clears it.")
	}
	return v, nil
}
func (t workspaceTx) migrationAuthor(ctx context.Context, f migrationFields, key, def string) (string, error) {
	id, e := f.text(key, def)
	if e != nil {
		return "", e
	}
	_, e = t.Account(ctx, id)
	return id, e
}
func checkMigrationFields(op string, in migration.Request) error {
	allowed, ok := migration.Fields[in.Kind]
	if !ok {
		return migration.Invalid("kind", "Unknown migration record kind.")
	}
	for k := range in.Fields {
		if !slices.Contains(allowed, k) {
			return migration.Invalid(k, fmt.Sprintf("Field %s is not writable for %s.", k, in.Kind))
		}
	}
	if op == "edit" {
		for _, k := range map[string][]string{"task": {"workspace_id"}, "comment": {"task_id", "reply_to"}, "memory": {"scope", "scope_id"}, "document": {"task_id"}, "activity": {"task_id"}}[in.Kind] {
			if _, ok := in.Fields[k]; ok {
				return migration.Invalid(k, "This relationship is immutable; omit it when editing.")
			}
		}
	}
	if len(in.Fields) == 0 {
		return migration.Invalid("fields", "Supply fields to create or edit.")
	}
	return nil
}
