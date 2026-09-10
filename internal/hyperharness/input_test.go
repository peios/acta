package hyperharness

import (
	"acta2/internal/harnesspipe"
	"acta2/internal/threads"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestProviderImageInputs(t *testing.T) {
	q := threads.Control{Text: "describe", Images: []threads.InputImage{{MediaType: "image/png", Base64: "YWJj", Name: "a.png"}}}
	codex := codexInput(q)
	if len(codex) != 2 || codex[1]["url"] != "data:image/png;base64,YWJj" {
		t.Fatalf("%v", codex)
	}
	raw, _ := json.Marshal(claudeInput(q.Text, q.Images))
	if !strings.Contains(string(raw), `"source":{"data":"YWJj","media_type":"image/png","type":"base64"}`) {
		t.Fatal(string(raw))
	}
	q.Text = ""
	if len(codexInput(q)) != 1 {
		t.Fatal("empty text part")
	}
	if claudeInput("Test Message", nil) != "Test Message" {
		t.Fatal("bootstrap changed")
	}
}
func TestRejectTextOnlyModelWithoutGuessingUnknownCapabilities(t *testing.T) {
	models := []threads.ModelOption{{ID: "text", Name: "Text model", InputModalities: []string{"text"}}, {ID: "vision", InputModalities: []string{"text", "image"}}, {ID: "unknown"}}
	if validateImageModel(models, "text") == nil {
		t.Fatal("text-only model accepted")
	}
	if validateImageModel(models, "vision") != nil || validateImageModel(models, "unknown") != nil {
		t.Fatal("incorrect capability rejection")
	}
}

type imageLimitedPipe struct {
	harnesspipe.API
	limit int
}

func (p imageLimitedPipe) InputLimit(context.Context) (int, error) { return p.limit, nil }
func TestOldPipeImageLimitIsRejectedBeforeDelivery(t *testing.T) {
	c := &Controller{pipe: imageLimitedPipe{limit: 1024 * 1024}}
	q := threads.Control{Images: []threads.InputImage{{MediaType: "image/png", Base64: strings.Repeat("a", 2*1024*1024)}}}
	if c.requireImageTransport(t.Context(), q) == nil {
		t.Fatal("old pipe accepted oversized input")
	}
	c.pipe = imageLimitedPipe{limit: harnesspipe.MaxInput}
	if err := c.requireImageTransport(t.Context(), q); err != nil {
		t.Fatal(err)
	}
}
