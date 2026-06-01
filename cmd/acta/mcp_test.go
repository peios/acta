package main

import (
	"strings"
	"testing"
)

func TestValidateAgentName(t *testing.T) {
	good := []string{"claude", "deploy-bot", "ci", "a1", "x-y-z"}
	for _, s := range good {
		if err := validateAgentName(s); err != nil {
			t.Errorf("validateAgentName(%q) = %v, want nil", s, err)
		}
	}
	bad := []string{"", "  ", "has space", "-lead", "trail-", "dou--ble", "under_score", "a/b"}
	for _, s := range bad {
		if err := validateAgentName(s); err == nil {
			t.Errorf("validateAgentName(%q) = nil, want error", s)
		}
	}
	// Mixed case is accepted (normalized to lowercase server-side).
	if err := validateAgentName("MixedCase"); err != nil {
		t.Errorf("validateAgentName(MixedCase) = %v, want nil (normalized)", err)
	}
}

func TestClaudeAddArgs(t *testing.T) {
	args := claudeAddArgs("https://acta.example/mcp", "Authorization: Bearer acta_pat_x")
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"mcp add", "--transport http", "--scope user",
		"acta https://acta.example/mcp", "--header",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("claudeAddArgs missing %q in: %s", want, joined)
		}
	}
	// The header (with its space) must be a single argv element, not split.
	last := args[len(args)-1]
	if last != "Authorization: Bearer acta_pat_x" {
		t.Errorf("header arg = %q, want it intact as one element", last)
	}
}

func TestHandleByID(t *testing.T) {
	agents := []agentResp{{ID: "a1", Handle: "jack/claude"}, {ID: "a2", Handle: "jack/ci"}}
	if got := handleByID(agents, "a2"); got != "jack/ci" {
		t.Errorf("handleByID a2 = %q, want jack/ci", got)
	}
	if got := handleByID(agents, "missing"); got != "missing" {
		t.Errorf("handleByID missing = %q, want the id back", got)
	}
}

func TestQuoteArgs(t *testing.T) {
	got := strings.Join(quoteArgs([]string{"add", "Authorization: Bearer x"}), " ")
	if got != `add "Authorization: Bearer x"` {
		t.Errorf("quoteArgs = %q", got)
	}
}

func TestCmdMCPDispatch(t *testing.T) {
	if err := cmdMCP(nil); err == nil {
		t.Error("cmdMCP(nil): want error for missing subcommand")
	}
	if err := cmdMCP([]string{"bogus"}); err == nil {
		t.Error("cmdMCP(bogus): want error for unknown subcommand")
	}
}
