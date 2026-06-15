package mcpcfg_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/peios/acta/internal/mcpcfg"
	"github.com/peios/acta/internal/store/memstore"
)

func newSvc(t *testing.T) (*mcpcfg.Service, context.Context) {
	t.Helper()
	return mcpcfg.New(memstore.New()), context.Background()
}

func TestRenderGuide(t *testing.T) {
	// The guide is hardcoded and ships with each release (no customisation), but
	// it renders a little live context — the workspace list — inline.
	g, err := mcpcfg.RenderGuide(mcpcfg.GuideData{
		Workspaces: []mcpcfg.GuideWorkspace{
			{Name: "Acta", Slug: "acta", ItemPrefix: "ACTA"},
			{Name: "Peios", Slug: "workspace"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Static markers the resource test and agents rely on.
	for _, want := range []string{"Using Acta", "memory_recall", "list_workspaces"} {
		if !strings.Contains(g, want) {
			t.Fatalf("guide missing %q", want)
		}
	}
	// The live workspace context is rendered inline.
	for _, want := range []string{"**Acta** — slug `acta`", "`ACTA-12`", "**Peios** — slug `workspace`"} {
		if !strings.Contains(g, want) {
			t.Fatalf("guide missing rendered workspace %q", want)
		}
	}

	// With no workspaces it still renders, with the empty-state line.
	empty, err := mcpcfg.RenderGuide(mcpcfg.GuideData{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(empty, "No workspaces exist yet") {
		t.Fatalf("empty guide missing the no-workspaces line")
	}
}

func TestParseArgs(t *testing.T) {
	got, err := mcpcfg.ParseArgs("workspace: which board (blank = default)\ntarget*: the thing\n\nbare")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 args, got %d", len(got))
	}
	if got[0].Name != "workspace" || got[0].Required {
		t.Fatalf("arg0 = %+v", got[0])
	}
	if got[1].Name != "target" || !got[1].Required || got[1].Description != "the thing" {
		t.Fatalf("arg1 = %+v", got[1])
	}
	if got[2].Name != "bare" || got[2].Description != "" {
		t.Fatalf("arg2 = %+v", got[2])
	}

	// Round-trips through FormatArgs.
	if again, err := mcpcfg.ParseArgs(mcpcfg.FormatArgs(got)); err != nil || len(again) != 3 {
		t.Fatalf("roundtrip: %v len=%d", err, len(again))
	}

	for _, bad := range []string{"Bad Name: x", "9lead: x", "dup: a\ndup: b"} {
		if _, err := mcpcfg.ParseArgs(bad); err == nil {
			t.Fatalf("expected error for %q", bad)
		}
	}
}

func TestCreatePromptValidation(t *testing.T) {
	s, ctx := newSvc(t)

	if _, err := s.CreatePrompt(ctx, mcpcfg.PromptInput{Name: "ok", Body: ""}); err == nil {
		t.Fatal("empty body should fail")
	}
	if _, err := s.CreatePrompt(ctx, mcpcfg.PromptInput{Name: "Bad Name", Body: "x"}); err == nil {
		t.Fatal("bad name should fail")
	}

	p, err := s.CreatePrompt(ctx, mcpcfg.PromptInput{Name: "release", Title: "Release", Body: "Ship {{version}}", ArgsText: "version*: the tag"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Position != 0 || len(p.Arguments) != 1 {
		t.Fatalf("created prompt = %+v", p)
	}

	// Duplicate name is a validation error, not a 500-class error.
	_, err = s.CreatePrompt(ctx, mcpcfg.PromptInput{Name: "release", Body: "x"})
	var ve mcpcfg.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("duplicate name should be a ValidationError, got %v", err)
	}
}

func TestEnsureSeededIsIdempotent(t *testing.T) {
	st := memstore.New()
	s := mcpcfg.New(st)
	ctx := context.Background()

	if err := s.EnsureSeeded(ctx); err != nil {
		t.Fatal(err)
	}
	first, _ := s.Prompts(ctx)
	if len(first) != len(mcpcfg.DefaultPrompts) {
		t.Fatalf("want %d seeded prompts, got %d", len(mcpcfg.DefaultPrompts), len(first))
	}

	// Deleting a seeded prompt and re-running must not resurrect it (seed-once).
	if err := s.DeletePrompt(ctx, first[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureSeeded(ctx); err != nil {
		t.Fatal(err)
	}
	after, _ := s.Prompts(ctx)
	if len(after) != len(first)-1 {
		t.Fatalf("re-seed resurrected a deleted prompt: %d", len(after))
	}
}

func TestSeedPromptsAreWellFormed(t *testing.T) {
	for _, p := range mcpcfg.DefaultPrompts {
		if _, err := mcpcfg.ParseArgs(mcpcfg.FormatArgs(p.Arguments)); err != nil {
			t.Fatalf("seed prompt %q args invalid: %v", p.Name, err)
		}
		if strings.TrimSpace(p.Body) == "" || p.Name == "" {
			t.Fatalf("seed prompt %q malformed", p.Name)
		}
	}
}
