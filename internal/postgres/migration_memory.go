package postgres

import (
	"acta/internal/memories"
	"acta/internal/migration"
	"context"
	"time"
)

func (t workspaceTx) migrationMemory(ctx context.Context, op string, in migration.Request, now time.Time) (string, error) {
	f := migrationFields(in.Fields)
	s := memories.Save{}
	target := ""
	created := now
	creator := t.actor.ID
	var e error
	if op == "edit" {
		old, e := t.MemoryGet(ctx, in.ID)
		if e != nil {
			return "", e
		}
		s = memories.Save{ID: old.ID, Revision: old.Revision, Scope: old.Scope, Key: old.Key, Summary: old.Summary, Content: old.Content}
		target = old.ScopeID
		created = old.CreatedAt
		creator = old.CreatedBy
	}
	for k, p := range map[string]*string{"scope": &s.Scope, "scope_id": &target, "key": &s.Key, "summary": &s.Summary, "content": &s.Content} {
		if *p, e = f.text(k, *p); e != nil {
			return "", e
		}
	}
	switch s.Scope {
	case "site":
		if target != "" {
			return "", migration.Invalid("scope_id", "Site memories have no scope ID.")
		}
	case "workspace":
		if _, e = t.Lookup(ctx, target, false); e != nil {
			return "", e
		}
		s.Workspace = &target
	case "user", "agent":
		a, e := t.Account(ctx, target)
		if e != nil {
			return "", e
		}
		if a.IsAgent() != (s.Scope == "agent") {
			return "", migration.Invalid("scope_id", "Account kind does not match scope.")
		}
	}
	if e = memories.Validate(&s); e != nil {
		return "", e
	}
	creator, e = t.migrationAuthor(ctx, f, "created_by", creator)
	if e != nil {
		return "", e
	}
	created, updated, e := f.dates(created, now)
	if e != nil {
		return "", e
	}
	v, e := t.MemorySave(ctx, s, target)
	if e != nil {
		return "", e
	}
	_, e = t.tx.Exec(ctx, `UPDATE memories SET created_by=$2,created_at=$3,updated_at=$4 WHERE id=$1`, v.ID, creator, created, updated)
	return v.ID, e
}
