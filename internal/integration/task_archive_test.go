package integration

import (
	"acta/internal/accounts"
	"acta/internal/auth"
	"acta/internal/comments"
	"acta/internal/documents"
	"acta/internal/tasks"
	"errors"
	"sync"
	"testing"
)

func TestTaskArchiveSubtreesSearchAndMutationBoundaries(t *testing.T) {
	root := securityDatabase(t)
	owner, f, w := workspaceOwner(t, root, "archive")
	ctx := t.Context()
	m := manager(f)
	create := func(title, parent string) tasks.Task {
		v, e := m.CreateTask(ctx, f.token, w.ID, tasks.Create{Title: title, ParentID: parent, Assignees: []string{owner.ID}})
		must(t, e)
		return v
	}
	get := func(id string) tasks.Task { v, e := m.Task(ctx, f.token, id); must(t, e); return v }
	toggle := func(v tasks.Task, state bool) tasks.Task {
		v, e := m.ArchiveTask(ctx, f.token, v.ID, tasks.Archive{Archived: state, Version: v.Versions["archived"]})
		must(t, e)
		return v
	}
	parent := create("Archive quarry parent", "")
	child := create("Archive quarry child", parent.ID)
	grand := create("Archive quarry grandchild", child.ID)
	separate := create("Archive quarry separate", parent.ID)
	comment := postComment(t, f, child.ID, "Keep this discussion", "")
	doc, e := m.SaveDocument(ctx, f.token, documents.Save{Task: child.ID, Title: "Keep document", Filename: "keep.md", Content: []byte("Document content")})
	must(t, e)
	must(t, m.SetTaskFollowing(ctx, f.token, child.ID, true))
	separate = toggle(separate, true)
	// An individually archived child is reachable from the separate archive roots.
	p, e := m.Tasks(ctx, f.token, w.ID, tasks.Filter{Archived: true, State: "all"})
	must(t, e)
	if len(p.Tasks) != 1 || p.Tasks[0].ID != separate.ID {
		t.Fatal(p)
	}
	parent = toggle(parent, true)
	for _, id := range []string{parent.ID, child.ID, grand.ID, separate.ID} {
		v := get(id)
		if !v.Archived || v.ArchivedAt == nil {
			t.Fatal(v)
		}
	}
	p, e = m.Tasks(ctx, f.token, w.ID, tasks.Filter{State: "all"})
	must(t, e)
	if len(p.Tasks) != 0 {
		t.Fatal(p)
	}
	p, e = m.Tasks(ctx, f.token, w.ID, tasks.Filter{Archived: true, State: "all"})
	must(t, e)
	if len(p.Tasks) != 1 || p.Tasks[0].ID != parent.ID {
		t.Fatal(p)
	}
	detail, e := m.InspectTask(ctx, f.token, parent.ID, "")
	must(t, e)
	if len(detail.Subtasks.Tasks) != 2 {
		t.Fatal(detail)
	}
	found := searchTasks(t, f, tasks.SearchQuery{Query: "quarry"})
	if len(found.Tasks) != 0 {
		t.Fatal(found)
	}
	found = searchTasks(t, f, tasks.SearchQuery{Query: "quarry", IncludeArchived: true})
	if len(found.Tasks) != 4 || !found.Tasks[0].Archived {
		t.Fatal(found)
	}
	blocked := func(e error) {
		t.Helper()
		var field *accounts.FieldError
		if !errors.As(e, &field) || field.Field != "archived" {
			t.Fatalf("expected archive validation, got %v", e)
		}
	}
	_, e = m.ArchiveTask(ctx, f.token, child.ID, tasks.Archive{Version: get(child.ID).Versions["archived"]})
	blocked(e)
	_, e = patchTask(t, f, get(child.ID), "title", "Forbidden edit")
	blocked(e)
	_, e = m.CreateTask(ctx, f.token, w.ID, tasks.Create{Title: "Forbidden child", ParentID: parent.ID})
	blocked(e)
	active := create("Active", "")
	_, e = patchTask(t, f, active, "parent_id", parent.ID)
	blocked(e)
	_, e = m.CreateComment(ctx, f.token, child.ID, comments.Create{Body: "Forbidden comment", RequestID: "c9cc4976-d66e-4717-bf6a-86a669bf42ab"})
	blocked(e)
	_, e = m.UpdateComment(ctx, f.token, child.ID, comment.ID, comments.Update{Version: 1, Body: "No"})
	blocked(e)
	c, e := m.Comment(ctx, f.token, child.ID, comment.ID)
	must(t, e)
	if c.Comment.CanEdit || c.Comment.CanDelete {
		t.Fatal(c)
	}
	_, e = m.SaveDocument(ctx, f.token, documents.Save{Task: child.ID, Title: "No", Filename: "no.md", Content: []byte("No")})
	blocked(e)
	d, e := m.Document(ctx, f.token, doc.ID)
	must(t, e)
	if d.CanWrite {
		t.Fatal(d)
	}
	_, b, e := m.DocumentFile(ctx, f.token, doc.ID, 0)
	must(t, e)
	if string(b) != "Document content" {
		t.Fatal(string(b))
	}
	following, e := m.TaskFollowing(ctx, f.token, child.ID)
	must(t, e)
	if !following {
		t.Fatal("lost follow")
	}
	before := get(child.ID)
	if len(before.Assignees) != 1 || before.StatusID != child.StatusID {
		t.Fatal("lost task data")
	}
	// Same requested state is idempotent, stale opposite-state writes conflict.
	same, e := m.ArchiveTask(ctx, f.token, parent.ID, tasks.Archive{Archived: true, Version: 1})
	must(t, e)
	if same.Versions["archived"] != parent.Versions["archived"] {
		t.Fatal(same)
	}
	_, e = m.ArchiveTask(ctx, f.token, parent.ID, tasks.Archive{Archived: false, Version: 1})
	var conflict *tasks.Conflict
	if !errors.As(e, &conflict) || conflict.Value != true {
		t.Fatal(e)
	}
	parent = toggle(parent, false)
	if get(child.ID).Archived || get(grand.ID).Archived || !get(separate.ID).Archived {
		t.Fatal("restoration boundaries")
	}
	toggle(separate, false)
	_, e = patchTask(t, f, get(child.ID), "title", "Restored child")
	must(t, e)
	reader, rf := permissionMember(t, root, "archive_reader")
	joinWorkspace(t, f, w, reader.ID)
	_, e = manager(rf).ArchiveTask(ctx, rf.token, parent.ID, tasks.Archive{Archived: true, Version: parent.Versions["archived"]})
	if !errors.Is(e, auth.ErrForbidden) {
		t.Fatal(e)
	}
}

func TestTaskArchiveConcurrentChildCreation(t *testing.T) {
	root := securityDatabase(t)
	_, f, w := workspaceOwner(t, root, "archive-race")
	ctx := t.Context()
	m := manager(f)
	p, e := m.CreateTask(ctx, f.token, w.ID, tasks.Create{Title: "Race"})
	must(t, e)
	var child tasks.Task
	var createErr, archiveErr error
	var wg sync.WaitGroup
	wg.Go(func() {
		child, createErr = m.CreateTask(ctx, f.token, w.ID, tasks.Create{Title: "Child", ParentID: p.ID})
	})
	wg.Go(func() { _, archiveErr = m.ArchiveTask(ctx, f.token, p.ID, tasks.Archive{Archived: true, Version: 1}) })
	wg.Wait()
	must(t, archiveErr)
	if createErr == nil {
		v, e := m.Task(ctx, f.token, child.ID)
		must(t, e)
		if !v.Archived {
			t.Fatal("active child stranded under archived parent")
		}
	} else {
		var field *accounts.FieldError
		if !errors.As(createErr, &field) || field.Field != "archived" {
			t.Fatal(createErr)
		}
	}
}
