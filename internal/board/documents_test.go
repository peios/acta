package board_test

import (
	"context"
	"strings"
	"testing"

	"github.com/peios/acta/internal/board"
	"github.com/peios/acta/internal/store"
	"github.com/peios/acta/internal/store/memstore"
)

// docFixture spins up a service over an in-memory store with one workspace, its
// default lanes, and a single item to hang documents off.
func docFixture(t *testing.T) (context.Context, *board.Service, store.Item) {
	t.Helper()
	ms := memstore.New()
	ctx := context.Background()
	svc := board.New(ms)
	ws, _ := ms.CreateWorkspace(ctx, store.Workspace{Slug: "a", Name: "A"})
	if err := svc.SeedDefaults(ctx, ws.ID); err != nil {
		t.Fatal(err)
	}
	sts, _ := svc.Statuses(ctx, ws.ID)
	it, err := svc.CreateItem(ctx, ws.ID, sts[0].ID, "task")
	if err != nil {
		t.Fatal(err)
	}
	return ctx, svc, it
}

func TestAddAndListDocument(t *testing.T) {
	ctx, svc, it := docFixture(t)
	d, err := svc.AddDocument(ctx, it.ID, "", "Compliance Report", "# Findings\n\nAll good.")
	if err != nil {
		t.Fatal(err)
	}
	if d.ID == "" || d.Title != "Compliance Report" {
		t.Fatalf("unexpected document: %+v", d)
	}
	docs, err := svc.Documents(ctx, it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 || docs[0].Body != "# Findings\n\nAll good." {
		t.Fatalf("documents = %+v", docs)
	}
}

func TestAddDocumentTrimsAndRequiresTitle(t *testing.T) {
	ctx, svc, it := docFixture(t)
	if _, err := svc.AddDocument(ctx, it.ID, "", "   ", "body"); err != board.ErrInvalidDocument {
		t.Fatalf("blank title: want ErrInvalidDocument, got %v", err)
	}
	d, err := svc.AddDocument(ctx, it.ID, "", "  Spaced  ", "")
	if err != nil {
		t.Fatal(err)
	}
	if d.Title != "Spaced" {
		t.Fatalf("title not trimmed: %q", d.Title)
	}
}

func TestAddDocumentRejectsOversize(t *testing.T) {
	ctx, svc, it := docFixture(t)
	longTitle := strings.Repeat("x", board.MaxDocumentTitleLen+1)
	if _, err := svc.AddDocument(ctx, it.ID, "", longTitle, ""); err != board.ErrInvalidDocument {
		t.Fatalf("oversize title: want ErrInvalidDocument, got %v", err)
	}
	longBody := strings.Repeat("y", board.MaxDocumentLen+1)
	if _, err := svc.AddDocument(ctx, it.ID, "", "ok", longBody); err != board.ErrInvalidDocument {
		t.Fatalf("oversize body: want ErrInvalidDocument, got %v", err)
	}
}

func TestEditDocument(t *testing.T) {
	ctx, svc, it := docFixture(t)
	d, _ := svc.AddDocument(ctx, it.ID, "", "Draft", "v1")
	upd, err := svc.EditDocument(ctx, d.ID, "Final", "v2")
	if err != nil {
		t.Fatal(err)
	}
	if upd.Title != "Final" || upd.Body != "v2" {
		t.Fatalf("edit = %+v", upd)
	}
	if upd.UpdatedAt.Before(d.CreatedAt) {
		t.Errorf("updatedAt %v should not predate createdAt %v", upd.UpdatedAt, d.CreatedAt)
	}
	got, _ := svc.Document(ctx, d.ID)
	if got.Title != "Final" || got.Body != "v2" {
		t.Fatalf("persisted = %+v", got)
	}
}

func TestEditDocumentValidatesAndFindsMissing(t *testing.T) {
	ctx, svc, it := docFixture(t)
	d, _ := svc.AddDocument(ctx, it.ID, "", "Doc", "body")
	if _, err := svc.EditDocument(ctx, d.ID, "  ", "x"); err != board.ErrInvalidDocument {
		t.Fatalf("blank title on edit: want ErrInvalidDocument, got %v", err)
	}
	if _, err := svc.EditDocument(ctx, "missing", "T", "B"); err != store.ErrDocumentNotFound {
		t.Fatalf("missing doc: want ErrDocumentNotFound, got %v", err)
	}
}

func TestRemoveDocument(t *testing.T) {
	ctx, svc, it := docFixture(t)
	d, _ := svc.AddDocument(ctx, it.ID, "", "Doc", "body")
	removed, err := svc.RemoveDocument(ctx, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if removed.ID != d.ID {
		t.Fatalf("removed = %+v", removed)
	}
	if docs, _ := svc.Documents(ctx, it.ID); len(docs) != 0 {
		t.Fatalf("want no documents, got %d", len(docs))
	}
	if _, err := svc.RemoveDocument(ctx, d.ID); err != store.ErrDocumentNotFound {
		t.Fatalf("double delete: want ErrDocumentNotFound, got %v", err)
	}
}

func TestDocumentsOrderedByCreation(t *testing.T) {
	ctx, svc, it := docFixture(t)
	a, _ := svc.AddDocument(ctx, it.ID, "", "A", "")
	b, _ := svc.AddDocument(ctx, it.ID, "", "B", "")
	docs, _ := svc.Documents(ctx, it.ID)
	if len(docs) != 2 || docs[0].ID != a.ID || docs[1].ID != b.ID {
		t.Fatalf("order = %+v", docs)
	}
}

func TestHumanizeDocumentEvents(t *testing.T) {
	cases := map[string]string{
		store.EventDocumentAdded:   "added the document “Report”",
		store.EventDocumentUpdated: "updated the document “Report”",
		store.EventDocumentRemoved: "removed the document “Report”",
	}
	for verb, want := range cases {
		got := board.HumanizeEvent(store.Event{Verb: verb, Data: map[string]string{"title": "Report"}})
		if got != want {
			t.Errorf("HumanizeEvent(%s) = %q, want %q", verb, got, want)
		}
	}
}
