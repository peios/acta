package integration

import (
	"acta/internal/accounts"
	"acta/internal/auth"
	"acta/internal/memories"
	ws "acta/internal/workspaces"
	"errors"
	"fmt"
	"sync"
	"testing"
)

func memoryInput(scope, key string) memories.Save {
	return memories.Save{Scope: scope, Key: key, Summary: "Durable convention", Content: "# Knowledge\nUseful detail"}
}
func TestMemoriesScopesAndConcurrentUpdates(t *testing.T) {
	root := securityDatabase(t)
	_, f, w := workspaceOwner(t, root, "memory-owner")
	other, outsider := permissionMember(t, root, "memory-other")
	ctx := t.Context()
	m := manager(f)
	in := memoryInput("workspace", "convention")
	in.Workspace = &w.ID
	shared, e := m.SaveMemory(ctx, f.token, in)
	must(t, e)
	own, e := m.SaveMemory(ctx, f.token, memoryInput("user", "convention"))
	must(t, e)
	site, e := manager(root).SaveMemory(ctx, root.token, memoryInput("site", "convention"))
	must(t, e)
	if _, e = m.SaveMemory(ctx, f.token, memoryInput("site", "bad")); e == nil {
		t.Fatal("ordinary user wrote site memory")
	}
	if _, e = m.SaveMemory(ctx, f.token, in); e == nil {
		t.Fatal("duplicate scoped key")
	}
	for _, id := range []string{shared.ID, own.ID, site.ID} {
		if _, e = manager(outsider).GetMemory(ctx, outsider.token, id); !errors.Is(e, auth.ErrNotFound) {
			t.Fatal("private read", id, e)
		}
	}
	w = joinWorkspace(t, f, w, other.ID)
	got, e := manager(outsider).GetMemory(ctx, outsider.token, shared.ID)
	must(t, e)
	if got.CanWrite {
		t.Fatal("plain member can write")
	}
	if _, e = manager(outsider).SaveMemory(ctx, outsider.token, in); !errors.Is(e, auth.ErrForbidden) {
		t.Fatal("plain member write", e)
	}
	page, e := m.RecallMemories(ctx, f.token, memories.Recall{Workspace: &w.Slug})
	must(t, e)
	if len(page.Memories) != 2 {
		t.Fatal(page)
	}
	for _, row := range page.Memories {
		if row.Content != "" {
			t.Fatal("recall leaked full body")
		}
	}
	updated := in
	updated.ID = shared.ID
	updated.Revision = shared.Revision
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := range 2 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			x := updated
			x.Content = fmt.Sprintf("Edit %d", i)
			_, e := m.SaveMemory(ctx, f.token, x)
			results <- e
		}(i)
	}
	wg.Wait()
	close(results)
	successes, conflicts := 0, 0
	for e := range results {
		if e == nil {
			successes++
		} else if errors.Is(e, memories.ErrConflict) {
			conflicts++
		} else {
			t.Fatal(e)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatal(successes, conflicts)
	}
	if e = m.DeleteMemory(ctx, f.token, shared.ID, 1); !errors.Is(e, memories.ErrConflict) {
		t.Fatal("stale delete", e)
	}
	current, e := m.GetMemory(ctx, f.token, shared.ID)
	must(t, e)
	must(t, m.DeleteMemory(ctx, f.token, shared.ID, current.Revision))
	if _, e = m.GetMemory(ctx, f.token, shared.ID); !errors.Is(e, auth.ErrNotFound) {
		t.Fatal(e)
	}
	w = workspaceGrants(t, f, w, other.ID, false, []string{ws.WriteMemories})
	can, e := manager(outsider).SaveMemory(ctx, outsider.token, in)
	must(t, e)
	w = workspaceGrants(t, f, w, other.ID, false, nil)
	if e = manager(outsider).DeleteMemory(ctx, outsider.token, can.ID, can.Revision); !errors.Is(e, auth.ErrForbidden) {
		t.Fatal("revoked write survived", e)
	}
}
func TestMemoriesAgentOwnershipAndRecallPagination(t *testing.T) {
	f := securityDatabase(t)
	ctx := t.Context()
	m := manager(f)
	own, e := m.SaveMemory(ctx, f.token, memoryInput("user", "preferences"))
	must(t, e)
	a := agentAccount(t, f, "memory-agent", nil)
	b := agentAccount(t, f, "memory-other-agent", nil)
	token := agentCLI(t, f, a.ID)
	otherToken := agentCLI(t, f, b.ID)
	got, e := m.GetMemory(ctx, token, own.ID)
	must(t, e)
	if got.CanWrite {
		t.Fatal("agent can write owner by default")
	}
	if _, e = m.SaveMemory(ctx, token, memoryInput("user", "wrong")); !errors.Is(e, auth.ErrForbidden) {
		t.Fatal(e)
	}
	agentMemory, e := m.SaveMemory(ctx, token, memoryInput("agent", "role"))
	must(t, e)
	if _, e = m.GetMemory(ctx, otherToken, agentMemory.ID); !errors.Is(e, auth.ErrNotFound) {
		t.Fatal("sibling agent read", e)
	}
	got, e = m.GetMemory(ctx, f.token, agentMemory.ID)
	must(t, e)
	if !got.CanWrite {
		t.Fatal("owner cannot manage")
	}
	page, e := m.RecallMemories(ctx, token, memories.Recall{})
	must(t, e)
	if len(page.Memories) != 2 {
		t.Fatal(page)
	}
	a, e = m.AgentPermissions(ctx, f.token, a.ID, a.PermissionsVersion, []string{accounts.WriteUserMemories})
	must(t, e)
	_, e = m.SaveMemory(ctx, token, memoryInput("user", "delegated"))
	must(t, e)
	a, e = m.AgentPermissions(ctx, f.token, a.ID, a.PermissionsVersion, nil)
	must(t, e)
	if _, e = m.SaveMemory(ctx, token, memoryInput("user", "revoked")); !errors.Is(e, auth.ErrForbidden) {
		t.Fatal("delegation revocation ignored", e)
	}
	for i := range 54 {
		_, e = m.SaveMemory(ctx, token, memoryInput("agent", fmt.Sprintf("page-%02d", i)))
		must(t, e)
	}
	page, e = m.RecallMemories(ctx, token, memories.Recall{Query: "page-"})
	must(t, e)
	if len(page.Memories) != 50 || page.Cursor == "" {
		t.Fatal(page)
	}
	second, e := m.RecallMemories(ctx, token, memories.Recall{Query: "page-", Cursor: page.Cursor})
	must(t, e)
	if len(second.Memories) != 4 || second.Cursor != "" {
		t.Fatal(second)
	}
	if _, e = m.RecallMemories(ctx, token, memories.Recall{Query: "different", Cursor: page.Cursor}); e == nil {
		t.Fatal("cursor query changed")
	}
	must(t, m.DisableAgent(ctx, f.token, a.ID, true))
	if _, e = m.GetMemory(ctx, token, own.ID); e == nil {
		t.Fatal("disabled agent read")
	}
}
