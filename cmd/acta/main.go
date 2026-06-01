// Command acta is the Acta client CLI: a thin HTTP client over the JSON API,
// authenticated by a personal access token. It's how a human at a terminal — or
// an agent process — sees the board and moves work.
//
// Configuration comes from the environment, the way an agent is wired:
//
//	ACTA_URL        server base URL (default http://localhost:8080)
//	ACTA_TOKEN      personal access token (required)
//	ACTA_WORKSPACE  default workspace slug (optional)
//
// Commands:
//
//	acta login [host] / acta logout
//	acta whoami
//	acta workspaces
//	acta board   [--workspace slug] [--json]
//	acta item new  [--workspace slug] [--status name | --parent id] <title>
//	acta item <id>                 show an item and its subtasks
//	acta item <id> status <name>   set the item's status
//
// Flags may appear before, after, or among the positional arguments.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	if err := run(os.Args[1], os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, "acta: "+err.Error())
		os.Exit(1)
	}
}

func run(cmd string, args []string) error {
	switch cmd {
	case "login":
		return cmdLogin(args)
	case "logout":
		return cmdLogout(args)
	case "whoami":
		return cmdWhoami(args)
	case "workspaces":
		return cmdWorkspaces(args)
	case "board":
		return cmdBoard(args)
	case "item":
		return cmdItem(args)
	case "mcp":
		return cmdMCP(args)
	case "help", "-h", "--help":
		usage()
		return nil
	default:
		return fmt.Errorf("unknown command %q (try: acta help)", cmd)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `acta — Acta client

Usage:
  acta login  [host]              authorize this machine in the browser
  acta logout                     revoke and forget this machine's token
  acta whoami
  acta workspaces
  acta board   [--workspace slug] [--json]
  acta item new  [--workspace slug] [--status name | --parent id] <title>
  acta item <id>                  show an item and its subtasks
  acta item <id> status <name>    set the item's status
  acta mcp install                wire an MCP client (Claude Code) to Acta

Environment (override the stored login):
  ACTA_URL        server base URL (default http://localhost:8080)
  ACTA_TOKEN      personal access token
  ACTA_WORKSPACE  default workspace slug (optional)
`)
}

// --- commands ---

func cmdWhoami(args []string) error {
	fs := flag.NewFlagSet("whoami", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "output raw JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	c, err := newClient()
	if err != nil {
		return err
	}
	data, err := c.do("GET", "/api/v1/me", nil)
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(data)
	}
	var me struct{ Username, Display string }
	_ = json.Unmarshal(data, &me)
	fmt.Printf("%s (%s)\n", me.Username, me.Display)
	return nil
}

func cmdWorkspaces(args []string) error {
	fs := flag.NewFlagSet("workspaces", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "output raw JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	c, err := newClient()
	if err != nil {
		return err
	}
	data, err := c.do("GET", "/api/v1/workspaces", nil)
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(data)
	}
	var list []struct{ Slug, Name string }
	if err := json.Unmarshal(data, &list); err != nil {
		return err
	}
	tw := newTable("SLUG", "NAME")
	for _, ws := range list {
		fmt.Fprintf(tw, "%s\t%s\n", ws.Slug, ws.Name)
	}
	return tw.Flush()
}

func cmdBoard(args []string) error {
	fs := flag.NewFlagSet("board", flag.ContinueOnError)
	ws := fs.String("workspace", "", "workspace slug")
	asJSON := fs.Bool("json", false, "output raw JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	c, err := newClient()
	if err != nil {
		return err
	}
	slug, err := c.workspaceSlug(*ws)
	if err != nil {
		return err
	}
	data, err := c.do("GET", "/api/v1/w/"+slug+"/items", nil)
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(data)
	}
	var items []item
	if err := json.Unmarshal(data, &items); err != nil {
		return err
	}
	tw := newTable("ID", "STATUS", "SUBS", "TITLE", "ASSIGNEE", "CREATED BY")
	for _, it := range items {
		title := it.Title
		if it.Milestone {
			title = "◆ " + title
		}
		subs := "-"
		if it.SubtasksTotal > 0 {
			subs = fmt.Sprintf("%d/%d", it.SubtasksDone, it.SubtasksTotal)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", it.ID, it.Status, subs, title, dash(it.Assignee), dash(it.CreatedBy))
	}
	return tw.Flush()
}

// cmdItem dispatches the item grammar: `item new …` creates; otherwise the
// first arg is an item id, optionally followed by an action (`status`).
// `item <id>` with no action shows it.
func cmdItem(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("item: need 'new' or an item id (e.g. acta item <id>)")
	}
	if args[0] == "new" {
		return cmdItemNew(args[1:])
	}
	id, rest := args[0], args[1:]
	if len(rest) > 0 && rest[0] == "status" {
		return cmdItemStatus(id, rest[1:])
	}
	return cmdItemShow(id, rest)
}

func cmdItemNew(args []string) error {
	fs := flag.NewFlagSet("item new", flag.ContinueOnError)
	ws := fs.String("workspace", "", "workspace slug")
	status := fs.String("status", "", "status name (defaults to the first lane)")
	parent := fs.String("parent", "", "create as a subtask of this item id")
	asJSON := fs.Bool("json", false, "output raw JSON")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	title := strings.TrimSpace(strings.Join(pos, " "))
	if title == "" {
		return fmt.Errorf("item new: a title is required")
	}
	c, err := newClient()
	if err != nil {
		return err
	}
	slug, err := c.workspaceSlug(*ws)
	if err != nil {
		return err
	}
	var data []byte
	if *parent != "" {
		// Subtasks start in the first lane (transition to move); --status is N/A.
		data, err = c.do("POST", "/api/v1/w/"+slug+"/items/"+*parent+"/subtasks", map[string]string{"title": title})
	} else {
		data, err = c.do("POST", "/api/v1/w/"+slug+"/items", map[string]string{"title": title, "status": *status})
	}
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(data)
	}
	var it item
	_ = json.Unmarshal(data, &it)
	what := "created"
	if *parent != "" {
		what = "created subtask"
	}
	fmt.Printf("%s %s  [%s]  %s\n", what, it.ID, it.Status, it.Title)
	return nil
}

func cmdItemShow(id string, args []string) error {
	fs := flag.NewFlagSet("item", flag.ContinueOnError)
	ws := fs.String("workspace", "", "workspace slug")
	asJSON := fs.Bool("json", false, "output raw JSON")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return fmt.Errorf("item %s: unexpected argument %q (did you mean `item %s status %s`?)", id, pos[0], id, pos[0])
	}
	c, err := newClient()
	if err != nil {
		return err
	}
	slug, err := c.workspaceSlug(*ws)
	if err != nil {
		return err
	}
	data, err := c.do("GET", "/api/v1/w/"+slug+"/items/"+id, nil)
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(data)
	}
	var it item
	if err := json.Unmarshal(data, &it); err != nil {
		return err
	}
	printItem(it)
	return nil
}

func printItem(it item) {
	fmt.Println(it.Title)
	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 1, ' ', 0)
	fmt.Fprintf(tw, "  id\t%s\n", it.ID)
	fmt.Fprintf(tw, "  status\t%s\n", it.Status)
	fmt.Fprintf(tw, "  assignee\t%s\n", dash(it.Assignee))
	fmt.Fprintf(tw, "  created by\t%s\n", dash(it.CreatedBy))
	if it.Milestone {
		fmt.Fprintf(tw, "  milestone\tyes\n")
	}
	if it.ParentID != "" {
		fmt.Fprintf(tw, "  parent\t%s\n", it.ParentID)
	}
	_ = tw.Flush()
	if len(it.Subtasks) > 0 {
		fmt.Printf("\nSubtasks (%d):\n", len(it.Subtasks))
		st := newTable("ID", "STATUS", "TITLE")
		for _, s := range it.Subtasks {
			title := s.Title
			if s.Milestone {
				title = "◆ " + title
			}
			fmt.Fprintf(st, "%s\t%s\t%s\n", s.ID, s.Status, title)
		}
		_ = st.Flush()
	}
}

func cmdItemStatus(id string, args []string) error {
	fs := flag.NewFlagSet("item status", flag.ContinueOnError)
	ws := fs.String("workspace", "", "workspace slug")
	asJSON := fs.Bool("json", false, "output raw JSON")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) < 1 {
		return fmt.Errorf("item %s status: need a status name", id)
	}
	status := strings.Join(pos, " ")
	c, err := newClient()
	if err != nil {
		return err
	}
	slug, err := c.workspaceSlug(*ws)
	if err != nil {
		return err
	}
	data, err := c.do("POST", "/api/v1/w/"+slug+"/items/"+id+"/transition", map[string]string{"status": status})
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(data)
	}
	var it item
	_ = json.Unmarshal(data, &it)
	fmt.Printf("%s -> %s\n", it.ID, it.Status)
	return nil
}

// --- client ---

type item struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	Status        string `json:"status"`
	Assignee      string `json:"assignee"`
	Milestone     bool   `json:"milestone"`
	CreatedBy     string `json:"created_by"`
	ParentID      string `json:"parent_id"`
	Subtasks      []item `json:"subtasks"`
	SubtasksDone  int    `json:"subtasks_done"`
	SubtasksTotal int    `json:"subtasks_total"`
}

type client struct {
	base  string
	token string
	hc    *http.Client
}

// newClient resolves the token and base URL: an explicit env var wins, then the
// stored login (acta login), then the localhost default.
func newClient() (*client, error) {
	cfg := loadConfig()
	token := os.Getenv("ACTA_TOKEN")
	if token == "" {
		token = cfg.Token
	}
	if token == "" {
		return nil, fmt.Errorf("not logged in — run `acta login <host>` (or set ACTA_TOKEN)")
	}
	base := os.Getenv("ACTA_URL")
	if base == "" {
		base = cfg.URL
	}
	if base == "" {
		base = "http://localhost:8080"
	}
	return &client{
		base:  strings.TrimRight(base, "/"),
		token: token,
		hc:    &http.Client{Timeout: 15 * time.Second},
	}, nil
}

func (c *client) do(method, path string, body any) ([]byte, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.base+path, rdr)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, serverError(resp.StatusCode, data)
	}
	return data, nil
}

// workspaceSlug resolves the workspace to act on: the explicit flag, then
// ACTA_WORKSPACE, then the first workspace the server reports.
func (c *client) workspaceSlug(flagVal string) (string, error) {
	if flagVal != "" {
		return flagVal, nil
	}
	if env := os.Getenv("ACTA_WORKSPACE"); env != "" {
		return env, nil
	}
	data, err := c.do("GET", "/api/v1/workspaces", nil)
	if err != nil {
		return "", err
	}
	var list []struct{ Slug string }
	if err := json.Unmarshal(data, &list); err != nil {
		return "", err
	}
	if len(list) == 0 {
		return "", fmt.Errorf("no workspaces; pass --workspace")
	}
	return list[0].Slug, nil
}

func serverError(code int, data []byte) error {
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(data, &e) == nil && e.Error != "" {
		return fmt.Errorf("%s (HTTP %d)", e.Error, code)
	}
	return fmt.Errorf("server returned HTTP %d", code)
}

// --- output ---

// parseArgs parses flags that may appear before, after, or among the positional
// arguments — the stdlib flag package otherwise stops at the first positional.
// It returns the positionals in order.
func parseArgs(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return pos, nil
		}
		pos = append(pos, rest[0])
		args = rest[1:]
	}
}

func newTable(headers ...string) *tabwriter.Writer {
	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, strings.Join(headers, "\t"))
	return tw
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func printJSON(data []byte) error {
	var buf bytes.Buffer
	if json.Indent(&buf, data, "", "  ") != nil {
		_, err := os.Stdout.Write(data)
		return err
	}
	buf.WriteByte('\n')
	_, err := buf.WriteTo(os.Stdout)
	return err
}
