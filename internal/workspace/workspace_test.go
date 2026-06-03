package workspace_test

import (
	"context"
	"errors"
	"testing"

	"github.com/peios/acta/internal/store"
	"github.com/peios/acta/internal/store/memstore"
	"github.com/peios/acta/internal/workspace"
)

func newService(t *testing.T) (*workspace.Service, *memstore.Store) {
	t.Helper()
	ms := memstore.New()
	return workspace.New(ms), ms
}

func TestCreateDerivesSlug(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()

	w, err := svc.Create(ctx, "My Team", "")
	if err != nil {
		t.Fatal(err)
	}
	if w.Slug != "my-team" {
		t.Fatalf("slug: want my-team, got %q", w.Slug)
	}
	if w.Name != "My Team" {
		t.Fatalf("name: want %q, got %q", "My Team", w.Name)
	}
}

func TestCreateDedupesSlug(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()

	// Two distinct names that slugify to the same base get distinct slugs.
	if _, err := svc.Create(ctx, "My Team", ""); err != nil {
		t.Fatal(err)
	}
	w2, err := svc.Create(ctx, "My-Team", "")
	if err != nil {
		t.Fatal(err)
	}
	if w2.Slug != "my-team-2" {
		t.Fatalf("second slug: want my-team-2, got %q", w2.Slug)
	}
}

func TestCreateRejectsDuplicateName(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()

	if _, err := svc.Create(ctx, "General", ""); err != nil {
		t.Fatal(err)
	}
	// Case-insensitive: "general" collides with "General".
	_, err := svc.Create(ctx, "general", "")
	if !errors.Is(err, store.ErrWorkspaceNameTaken) {
		t.Fatalf("want ErrWorkspaceNameTaken, got %v", err)
	}
}

func TestCreateRejectsInvalidName(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()

	for _, name := range []string{"", "   ", string(make([]rune, workspace.MaxNameLen+1))} {
		if _, err := svc.Create(ctx, name, ""); !errors.Is(err, workspace.ErrInvalidName) {
			t.Fatalf("name %q: want ErrInvalidName, got %v", name, err)
		}
	}
}

func TestSlugFallbackForSymbolName(t *testing.T) {
	svc, _ := newService(t)
	w, err := svc.Create(context.Background(), "!!!", "")
	if err != nil {
		t.Fatal(err)
	}
	if w.Slug != "workspace" {
		t.Fatalf("slug: want workspace, got %q", w.Slug)
	}
}

func TestRenameKeepsSlug(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()

	w, err := svc.Create(ctx, "Engineering", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Rename(ctx, w.ID, "Platform"); err != nil {
		t.Fatal(err)
	}
	got, err := svc.ByID(ctx, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Platform" {
		t.Fatalf("name: want Platform, got %q", got.Name)
	}
	if got.Slug != w.Slug {
		t.Fatalf("slug changed on rename: was %q, now %q", w.Slug, got.Slug)
	}
}

func TestDeleteRefusesLastWorkspace(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()

	only, err := svc.Create(ctx, "General", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(ctx, only.ID); !errors.Is(err, workspace.ErrLastWorkspace) {
		t.Fatalf("deleting the only workspace: want ErrLastWorkspace, got %v", err)
	}

	// With a second present, deleting one is allowed.
	if _, err := svc.Create(ctx, "Engineering", ""); err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(ctx, only.ID); err != nil {
		t.Fatalf("deleting a non-last workspace: %v", err)
	}
	n, _ := svc.List(ctx)
	if len(n) != 1 {
		t.Fatalf("want 1 workspace left, got %d", len(n))
	}
}

func TestCreateDerivesPrefix(t *testing.T) {
	cases := map[string]string{
		"Acta":            "ACT", // 1 word -> first three letters
		"General":         "GEN",
		"X":               "X",   // 1 short word -> what's there
		"My Team":         "MYT", // 2 words -> w0[0], w0[1], w1[0]
		"Platform Team":   "PLT",
		"Hi There":        "HIT",
		"Foo Bar Baz Qux": "FBB", // 3+ words -> first letter of first three
	}
	for name, want := range cases {
		svc, _ := newService(t)
		w, err := svc.Create(context.Background(), name, "")
		if err != nil {
			t.Fatalf("%q: %v", name, err)
		}
		if w.ItemPrefix != want {
			t.Errorf("prefix for %q: want %q, got %q", name, want, w.ItemPrefix)
		}
	}
}

func TestCreateDedupesPrefix(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()

	// "Acta" and "Action" both derive ACT; the second falls through to ACT2.
	if _, err := svc.Create(ctx, "Acta", ""); err != nil {
		t.Fatal(err)
	}
	w2, err := svc.Create(ctx, "Action", "")
	if err != nil {
		t.Fatal(err)
	}
	if w2.ItemPrefix != "ACT2" {
		t.Fatalf("second prefix: want ACT2, got %q", w2.ItemPrefix)
	}
}

func TestUpdateChangesPrefix(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()

	w, err := svc.Create(ctx, "Acta", "")
	if err != nil {
		t.Fatal(err)
	}
	// A typed prefix is normalised (uppercased, symbols dropped).
	if err := svc.Update(ctx, w.ID, "Acta", "", "wrk-1!"); err != nil {
		t.Fatal(err)
	}
	got, _ := svc.ByID(ctx, w.ID)
	if got.ItemPrefix != "WRK1" {
		t.Fatalf("prefix: want WRK1, got %q", got.ItemPrefix)
	}
	// It now resolves by prefix.
	if bp, err := svc.ByPrefix(ctx, "wrk1"); err != nil || bp.ID != w.ID {
		t.Fatalf("ByPrefix(wrk1): want the workspace, got %v / %v", bp.ID, err)
	}
}

func TestUpdateRejectsTakenAndInvalidPrefix(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()

	a, _ := svc.Create(ctx, "Acta", "") // prefix ACT
	b, _ := svc.Create(ctx, "Beta", "") // prefix BET

	// Taking another workspace's prefix is refused.
	if err := svc.Update(ctx, b.ID, "Beta", "", "ACT"); !errors.Is(err, store.ErrWorkspacePrefixTaken) {
		t.Fatalf("want ErrWorkspacePrefixTaken, got %v", err)
	}
	// A prefix with no usable characters is refused.
	if err := svc.Update(ctx, b.ID, "Beta", "", "!!!"); !errors.Is(err, workspace.ErrInvalidPrefix) {
		t.Fatalf("want ErrInvalidPrefix, got %v", err)
	}
	// A name-only edit (no prefix field) leaves the prefix untouched.
	if err := svc.Update(ctx, a.ID, "Acta Renamed", "", ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := svc.ByID(ctx, a.ID); got.ItemPrefix != "ACT" {
		t.Fatalf("name-only edit changed prefix to %q", got.ItemPrefix)
	}
}
