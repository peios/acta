package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
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

func TestCodexAddArgs(t *testing.T) {
	args := codexAddArgs("codex")
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"mcp add acta", "-- acta mcp proxy codex",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("codexAddArgs missing %q in: %s", want, joined)
		}
	}
}

func TestNormalizeTokenLabel(t *testing.T) {
	if got := normalizeTokenLabel(" custom ", mcpClientCodex, "host"); got != "custom" {
		t.Errorf("normalizeTokenLabel custom = %q, want custom", got)
	}
	if got := normalizeTokenLabel("  ", mcpClientCodex, "host"); got != "codex@host" {
		t.Errorf("normalizeTokenLabel empty = %q, want codex@host", got)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestMCPProxyForward(t *testing.T) {
	id, err := jsonrpc.MakeID(float64(7))
	if err != nil {
		t.Fatal(err)
	}
	req := &jsonrpc.Request{ID: id, Method: "tools/list"}

	var sawAuth, sawBody bool
	proxy := &mcpHTTPProxy{
		endpoint: "https://acta.example/mcp",
		token:    "acta_pat_x",
		client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.String() != "https://acta.example/mcp" {
				t.Errorf("url = %s", r.URL)
			}
			sawAuth = r.Header.Get("Authorization") == "Bearer acta_pat_x"
			body, _ := io.ReadAll(r.Body)
			sawBody = strings.Contains(string(body), `"method":"tools/list"`)
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"jsonrpc":"2.0","id":7,"result":{"tools":[]}}`)),
			}, nil
		})},
	}
	reply, ok, err := proxy.forward(context.Background(), req)
	if err != nil {
		t.Fatalf("forward: %v", err)
	}
	if !ok {
		t.Fatal("forward returned no reply")
	}
	if !sawAuth {
		t.Error("proxy did not set bearer auth")
	}
	if !sawBody {
		t.Error("proxy did not forward the JSON-RPC request body")
	}
	resp, ok := reply.(*jsonrpc.Response)
	if !ok || resp.ID.Raw() != int64(7) && resp.ID.Raw() != float64(7) {
		t.Fatalf("reply = %#v, want response id 7", reply)
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
