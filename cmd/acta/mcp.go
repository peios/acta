package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"charm.land/huh/v2"
)

// cmdMCP dispatches the `acta mcp` group. Today its only subcommand is install,
// which wires a local MCP client to this Acta server end to end:
// authorize, pick or create a principal to act as, mint that principal's token,
// and write the client's config.
func cmdMCP(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("mcp: need a subcommand (try: acta mcp install)")
	}
	switch args[0] {
	case "install":
		return cmdMCPInstall(args[1:])
	case "proxy":
		return cmdMCPProxy(args[1:])
	case "-h", "--help", "help":
		fmt.Fprintln(os.Stderr, "usage: acta mcp install        wire an MCP client to your logged-in server")
		fmt.Fprintln(os.Stderr, "       acta mcp proxy [profile] bridge stdio MCP to Acta's HTTP MCP endpoint")
		return nil
	default:
		return fmt.Errorf("unknown mcp subcommand %q (try: acta mcp install)", args[0])
	}
}

const (
	principalSelf = "__self__"
	principalNew  = "__new__"

	mcpClientClaude = "claude"
	mcpClientCodex  = "codex"
)

func cmdMCPInstall(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("mcp install takes no arguments — it uses your `acta login` session (run `acta login <host>` to point at a server)")
	}
	// The installer creates agents and mints their tokens, which are human-only
	// actions, so it rides your interactive login: the stored token is always a
	// human's (acta login authenticates as you), never an agent's, and its URL is
	// the server to install against.
	cfg := loadConfig()
	if cfg.Token == "" {
		return fmt.Errorf("not logged in — run `acta login <host>` first, then `acta mcp install`")
	}
	base := strings.TrimRight(cfg.URL, "/")
	if base == "" {
		base = "http://localhost:8080"
	}
	c := &client{base: base, token: cfg.Token, hc: &http.Client{Timeout: 15 * time.Second}}

	me, err := c.me()
	if err != nil {
		return err
	}
	agents, err := c.listAgents()
	if err != nil {
		return err
	}

	// Choose the MCP client to configure, then the principal it will act as and
	// the label for the freshly minted token.
	mcpClient := defaultMCPClient()
	harnessForm := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title("Install into which harness?").
			Options(
				huh.NewOption("Claude Code", mcpClientClaude),
				huh.NewOption("Codex", mcpClientCodex),
			).
			Value(&mcpClient),
	))
	if err := runForm(harnessForm); err != nil {
		return err
	}

	principal := principalSelf
	if len(agents) > 0 {
		principal = agents[0].ID // default to acting as an agent when one exists
	}
	host, _ := os.Hostname()
	if host == "" {
		host = "local"
	}
	label := mcpClient + "@" + host

	opts := []huh.Option[string]{huh.NewOption("Yourself — "+me.Username, principalSelf)}
	for _, a := range agents {
		opts = append(opts, huh.NewOption(a.Handle, a.ID))
	}
	opts = append(opts, huh.NewOption("Create a new agent…", principalNew))

	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title("Act as which principal?").
			Description("An agent records itself as the author of what it does — recommended.").
			Options(opts...).
			Value(&principal),
		huh.NewInput().
			Title("Token label").
			Description("Shown in Acta's token list; identifies this machine.").
			Value(&label),
	))
	if err := runForm(form); err != nil {
		return err
	}
	label = normalizeTokenLabel(label, mcpClient, host)

	newName := ""
	if principal == principalNew {
		nameForm := huh.NewForm(huh.NewGroup(
			huh.NewInput().
				Title("New agent name").
				Description("Becomes " + me.Username + "/<name> — lowercase letters, digits, single hyphens.").
				Value(&newName).
				Validate(validateAgentName),
		))
		if err := runForm(nameForm); err != nil {
			return err
		}
	}

	// Resolve the principal to a freshly minted token.
	var tok tokenResp
	var actingAs string
	switch principal {
	case principalSelf:
		tok, err = c.mintSelfToken(label)
		actingAs = me.Username
	case principalNew:
		ag, e := c.createAgent(strings.ToLower(strings.TrimSpace(newName)))
		if e != nil {
			return e
		}
		tok, err = c.mintAgentToken(ag.ID, label)
		actingAs = ag.Handle
	default:
		tok, err = c.mintAgentToken(principal, label)
		actingAs = handleByID(agents, principal)
	}
	if err != nil {
		return err
	}

	switch mcpClient {
	case mcpClientClaude:
		if err := installClaude(base, tok.Token); err != nil {
			return err
		}
		fmt.Printf("\n✓ Wired Claude Code to %s, acting as %s.\n", base, actingAs)
		fmt.Println("  Check it in Claude Code with /mcp. The token is stored in Claude's")
		fmt.Println("  config and won't be shown again.")
		// Also set up the SessionStart hook that loads the agent's Acta memory
		// index each session. Best-effort: a failure here shouldn't fail the
		// install — the MCP server still works without it.
		if scriptPath, herr := installClaudeBootHook(); herr != nil {
			fmt.Fprintf(os.Stderr, "  ⚠ Couldn't set up the session-start memory hook: %v\n", herr)
			fmt.Fprintln(os.Stderr, "    (Acta still works — you just won't get your memory auto-loaded at startup.)")
		} else {
			fmt.Println("  Installed a SessionStart hook that loads your Acta memory index")
			fmt.Printf("  each session: %s\n", scriptPath)
			fmt.Println("  It takes effect next session; review or remove it with /hooks.")
		}
	case mcpClientCodex:
		cfg.MCPProfile("codex", mcpConfig{URL: base, Token: tok.Token})
		if err := saveConfig(cfg); err != nil {
			return err
		}
		if err := installCodex(base); err != nil {
			return err
		}
		fmt.Printf("\n✓ Wired Codex to %s, acting as %s.\n", base, actingAs)
		fmt.Println("  Codex will launch `acta mcp proxy codex` automatically.")
		fmt.Println("  The MCP token is stored in Acta's config and won't be shown again.")
	default:
		return fmt.Errorf("unknown MCP client %q", mcpClient)
	}
	return nil
}

func defaultMCPClient() string {
	if _, err := exec.LookPath("claude"); err == nil {
		return mcpClientClaude
	}
	if _, err := exec.LookPath("codex"); err == nil {
		return mcpClientCodex
	}
	return mcpClientClaude
}

func normalizeTokenLabel(label, mcpClient, host string) string {
	label = strings.TrimSpace(label)
	if label != "" {
		return label
	}
	return mcpClient + "@" + host
}

// installCodex writes the MCP server entry into Codex via its own CLI. Codex's
// stdio config launches Acta's proxy command, which reads the bearer token from
// Acta's own config and forwards to the HTTP MCP endpoint.
func installCodex(base string) error {
	addArgs := codexAddArgs("codex")

	codex, err := exec.LookPath("codex")
	if err != nil {
		fmt.Fprintln(os.Stderr, "\nThe `codex` CLI wasn't found on PATH. Finish by running:")
		fmt.Fprintln(os.Stderr, "  codex mcp remove acta")
		fmt.Fprintf(os.Stderr, "  codex %s\n", strings.Join(quoteArgs(addArgs), " "))
		return nil
	}
	// Best-effort removal of any existing entry, so add is idempotent.
	_ = exec.Command(codex, "mcp", "remove", "acta").Run()

	cmd := exec.Command(codex, addArgs...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("`codex mcp add` failed: %w (if an 'acta' server already exists, remove it with `codex mcp remove acta` and retry)", err)
	}
	return nil
}

// codexAddArgs builds the argv for `codex mcp add` for a stdio MCP server.
// Codex starts this command automatically when it needs the server. Under a
// login profile the stored command carries it, so the proxy reads the same
// config the install wrote to.
func codexAddArgs(mcpProfile string) []string {
	args := []string{"mcp", "add", "acta", "--", "acta"}
	if profile != "" {
		args = append(args, "--profile", profile)
	}
	return append(args, "mcp", "proxy", mcpProfile)
}

// installClaude writes the MCP server entry into Claude Code via its own CLI
// (so we never hand-edit its config), removing any prior "acta" entry first so a
// re-run replaces rather than collides. If the `claude` CLI isn't on PATH, it
// prints the exact command to run instead.
func installClaude(base, token string) error {
	url := strings.TrimRight(base, "/") + "/mcp"
	header := "Authorization: Bearer " + token
	addArgs := claudeAddArgs(url, header)

	claude, err := exec.LookPath("claude")
	if err != nil {
		fmt.Fprintln(os.Stderr, "\nThe `claude` CLI wasn't found on PATH. Finish by running:")
		fmt.Fprintf(os.Stderr, "  claude %s\n", strings.Join(quoteArgs(addArgs), " "))
		return nil
	}
	// Best-effort removal of any existing entry, so add is idempotent.
	_ = exec.Command(claude, "mcp", "remove", "acta").Run()

	cmd := exec.Command(claude, addArgs...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("`claude mcp add` failed: %w (if an 'acta' server already exists, remove it with `claude mcp remove acta` and retry)", err)
	}
	return nil
}

// claudeAddArgs builds the argv for `claude mcp add` for an HTTP MCP server at
// user scope. Split out so it can be tested without invoking claude.
func claudeAddArgs(url, header string) []string {
	return []string{"mcp", "add", "--transport", "http", "--scope", "user", "acta", url, "--header", header}
}

// --- API helpers (installer-specific reads/writes over the JSON API) ---

type meResp struct {
	Username string `json:"username"`
	Display  string `json:"display"`
}

type agentResp struct {
	ID     string `json:"id"`
	Handle string `json:"handle"`
	Name   string `json:"name"`
}

type tokenResp struct {
	Token  string `json:"token"`
	Prefix string `json:"prefix"`
}

func (c *client) me() (meResp, error) {
	data, err := c.do("GET", "/api/v1/me", nil)
	if err != nil {
		return meResp{}, err
	}
	var m meResp
	return m, json.Unmarshal(data, &m)
}

func (c *client) listAgents() ([]agentResp, error) {
	data, err := c.do("GET", "/api/v1/agents", nil)
	if err != nil {
		return nil, err
	}
	var a []agentResp
	return a, json.Unmarshal(data, &a)
}

func (c *client) createAgent(name string) (agentResp, error) {
	data, err := c.do("POST", "/api/v1/agents", map[string]string{"name": name})
	if err != nil {
		return agentResp{}, err
	}
	var a agentResp
	return a, json.Unmarshal(data, &a)
}

func (c *client) mintAgentToken(agentID, label string) (tokenResp, error) {
	data, err := c.do("POST", "/api/v1/agents/"+agentID+"/tokens", map[string]string{"name": label})
	if err != nil {
		return tokenResp{}, err
	}
	var t tokenResp
	return t, json.Unmarshal(data, &t)
}

func (c *client) mintSelfToken(label string) (tokenResp, error) {
	data, err := c.do("POST", "/api/v1/tokens", map[string]string{"name": label})
	if err != nil {
		return tokenResp{}, err
	}
	var t tokenResp
	return t, json.Unmarshal(data, &t)
}

// --- small helpers ---

// runForm runs a huh form, translating a user abort into a clean message and a
// non-interactive terminal into actionable guidance.
func runForm(f *huh.Form) error {
	err := f.Run()
	switch {
	case err == nil:
		return nil
	case errors.Is(err, huh.ErrUserAborted):
		return fmt.Errorf("cancelled")
	default:
		return fmt.Errorf("this command needs an interactive terminal: %w", err)
	}
}

var agentNameRe = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

func validateAgentName(s string) error {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return fmt.Errorf("a name is required")
	}
	if !agentNameRe.MatchString(s) {
		return fmt.Errorf("lowercase letters, digits, and single hyphens only")
	}
	return nil
}

func handleByID(agents []agentResp, id string) string {
	for _, a := range agents {
		if a.ID == id {
			return a.Handle
		}
	}
	return id
}

// quoteArgs shell-quotes args that contain spaces, for printing a runnable
// command (used only in the "claude not found" fallback).
func quoteArgs(args []string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		if strings.ContainsAny(a, " \t") {
			out[i] = `"` + a + `"`
		} else {
			out[i] = a
		}
	}
	return out
}
