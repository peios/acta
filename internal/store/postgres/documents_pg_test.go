package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/peios/acta/internal/store"
)

// TestPGDocuments exercises the documents table CRUD against real Postgres:
// create, list (oldest-first), get, update-in-place (title+body+updatedAt),
// hard delete, the not-found error, and the item-delete cascade.
func TestPGDocuments(t *testing.T) {
	pg := openTestDB(t)
	ctx := context.Background()

	ws, err := pg.CreateWorkspace(ctx, store.Workspace{Slug: "doc", Name: "Docs"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pg.DeleteWorkspace(ctx, ws.ID) })
	bd, err := pg.CreateBoard(ctx, store.Board{WorkspaceID: ws.ID, Name: "Tasks", Slug: "tasks", Position: 0})
	if err != nil {
		t.Fatal(err)
	}
	st, err := pg.CreateStatus(ctx, store.Status{WorkspaceID: ws.ID, BoardID: bd.ID, Name: "To do"})
	if err != nil {
		t.Fatal(err)
	}
	it, err := pg.CreateItem(ctx, store.Item{WorkspaceID: ws.ID, StatusID: st.ID, Title: "Audit"})
	if err != nil {
		t.Fatal(err)
	}
	author, err := pg.CreateUser(ctx, store.NewUser{Username: "doc_ada", Display: "Ada"})
	if err != nil {
		t.Fatal(err)
	}

	// Create two; they list oldest-first.
	a, err := pg.CreateDocument(ctx, store.Document{ItemID: it.ID, AuthorID: author.ID, Title: "Report A", Body: "# A"})
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == "" || a.AuthorID != author.ID || a.CreatedAt.IsZero() || a.UpdatedAt.IsZero() {
		t.Fatalf("create A returned %+v", a)
	}
	b, err := pg.CreateDocument(ctx, store.Document{ItemID: it.ID, Title: "Report B", Body: "# B"})
	if err != nil {
		t.Fatal(err)
	}
	if b.AuthorID != "" {
		t.Errorf("absent author should scan as empty, got %q", b.AuthorID)
	}

	docs, err := pg.DocumentsByItem(ctx, it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 2 || docs[0].ID != a.ID || docs[1].ID != b.ID {
		t.Fatalf("list order = %+v", docs)
	}

	// Get one.
	got, err := pg.DocumentByID(ctx, a.ID)
	if err != nil || got.Title != "Report A" || got.Body != "# A" {
		t.Fatalf("get A = %+v err=%v", got, err)
	}

	// Update in place: title + body + updatedAt advance, createdAt is unchanged.
	when := a.CreatedAt.Add(time.Minute).UTC()
	upd, err := pg.UpdateDocument(ctx, a.ID, "Report A (final)", "# A2", when)
	if err != nil {
		t.Fatal(err)
	}
	if upd.Title != "Report A (final)" || upd.Body != "# A2" {
		t.Fatalf("update returned %+v", upd)
	}
	if !upd.UpdatedAt.Equal(when) {
		t.Errorf("updatedAt = %v, want %v", upd.UpdatedAt, when)
	}
	if !upd.CreatedAt.Equal(a.CreatedAt) {
		t.Errorf("createdAt changed: %v -> %v", a.CreatedAt, upd.CreatedAt)
	}

	// Delete one; the other survives; deleting again is ErrDocumentNotFound.
	if err := pg.DeleteDocument(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := pg.DeleteDocument(ctx, a.ID); !errors.Is(err, store.ErrDocumentNotFound) {
		t.Fatalf("double delete: want ErrDocumentNotFound, got %v", err)
	}
	if _, err := pg.DocumentByID(ctx, a.ID); !errors.Is(err, store.ErrDocumentNotFound) {
		t.Fatalf("get deleted: want ErrDocumentNotFound, got %v", err)
	}

	// Deleting the item cascades to its remaining documents.
	if err := pg.DeleteItem(ctx, it.ID); err != nil {
		t.Fatal(err)
	}
	if docs, err := pg.DocumentsByItem(ctx, it.ID); err != nil || len(docs) != 0 {
		t.Fatalf("after item delete: docs=%+v err=%v", docs, err)
	}
}
