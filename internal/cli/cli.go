// Package cli owns command UX and profile selection, not account policy.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"

	"acta/internal/client"
	"charm.land/huh/v2"
	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"
)

type App struct {
	repo        Repository
	vault       Vault
	profile     string
	json        bool
	input       io.Reader
	out, errOut io.Writer
	interactive bool
}

func NewCommand(in *os.File, out, errOut *os.File) (*cobra.Command, error) {
	dir, err := configDir()
	if err != nil {
		return nil, err
	}
	a := &App{repo: Repository{dir}, vault: newVault(dir), input: in, out: out, errOut: errOut, interactive: term.IsTerminal(in.Fd()) && term.IsTerminal(errOut.Fd())}
	return a.command(), nil
}
func (a *App) command() *cobra.Command {
	root := &cobra.Command{Use: "acta", Short: "Acta for your terminal", SilenceUsage: true, SilenceErrors: true}
	root.SetIn(a.input)
	root.SetOut(a.out)
	root.SetErr(a.errOut)
	root.PersistentFlags().StringVarP(&a.profile, "profile", "p", "", "Use this profile instead of the active profile")
	root.PersistentFlags().BoolVar(&a.json, "json", false, "Write structured JSON output")
	root.AddCommand(a.loginCommand(), a.profileCommand(), a.taskCommand(), a.documentCommand(), a.workspaceCommand(), a.harnessCommand(), a.pipeCommand())
	status := &cobra.Command{Use: "status", Aliases: []string{"whoami"}, Short: "Show the selected server, account and credential source", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error { return a.status(cmd.Context()) }}
	logout := &cobra.Command{Use: "logout", Short: "Revoke the selected session and remove its saved credential", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error { return a.logout(cmd.Context()) }}
	root.AddCommand(status, logout)
	_ = root.RegisterFlagCompletionFunc("profile", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		c, e := a.repo.Load()
		if e != nil {
			return nil, cobra.ShellCompDirectiveError
		}
		return names(c), cobra.ShellCompDirectiveNoFileComp
	})
	return root
}
func names(c Config) []string {
	out := []string{}
	for n := range c.Profiles {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
func (a *App) selected() (string, Profile, error) {
	c, e := a.repo.Load()
	if e != nil {
		return "", Profile{}, e
	}
	n := a.profile
	if n == "" {
		n = c.Active
	}
	p, ok := c.Profiles[n]
	if !ok {
		return n, p, fmt.Errorf("Profile %q does not exist; use acta profile add %s", n, n)
	}
	return n, p, nil
}
func (a *App) credential(p Profile) (string, string, error) {
	if token := os.Getenv("ACTA_TOKEN"); token != "" {
		return token, "ACTA_TOKEN", nil
	}
	token, e := a.vault.Read(p)
	source := p.Storage
	if source == "file" {
		source, _ = a.vault.path(p.Credential)
	}
	if source == "" {
		source = "none"
	}
	return token, source, e
}
func (a *App) emit(value any, text string) error {
	if a.json {
		return json.NewEncoder(a.out).Encode(value)
	}
	_, e := fmt.Fprintln(a.out, text)
	return e
}
func (a *App) notice(text string) {
	if a.json {
		_ = json.NewEncoder(a.errOut).Encode(map[string]string{"notice": text})
	} else {
		fmt.Fprintln(a.errOut, text)
	}
}
func (a *App) prompt(ctx context.Context, fields ...huh.Field) error {
	if !a.interactive {
		return errors.New("This decision requires an interactive terminal; supply the explicit command flags")
	}
	return huh.NewForm(huh.NewGroup(fields...)).WithInput(a.input).WithOutput(a.errOut).WithAccessible(os.Getenv("ACTA_ACCESSIBLE") != "").RunWithContext(ctx)
}
func (a *App) confirm(ctx context.Context, title string) (bool, error) {
	v := false
	e := a.prompt(ctx, huh.NewConfirm().Title(title).Affirmative("Yes").Negative("Cancel").Value(&v))
	return v, e
}
func (a *App) status(ctx context.Context) error {
	name, p, err := a.selected()
	if err != nil {
		return err
	}
	token, source, err := a.credential(p)
	if err != nil {
		return fmt.Errorf("read credential: %w", err)
	}
	if p.URL == "" {
		return errors.New("No server configured; run acta login <server>")
	}
	if token == "" {
		return errors.New("Not signed in; run acta login")
	}
	c := client.New(p.URL, token)
	account, err := c.Account(ctx)
	if err != nil {
		return err
	}
	text := fmt.Sprintf("Profile: %s\nServer: %s\nAccount: @%s\nCredential: %s", name, p.URL, account.Username, source)
	if account.MFARequired {
		text += "\nMFA setup is required in the browser before using Acta."
	}
	return a.emit(map[string]any{"profile": name, "server": p.URL, "credential_source": source, "account": account}, text)
}
func (a *App) logout(ctx context.Context) error {
	name, p, err := a.selected()
	if err != nil {
		return err
	}
	token, source, err := a.credential(p)
	if err != nil {
		return err
	}
	if token != "" {
		if p.URL == "" {
			return errors.New("No server configured for revocation")
		}
		if err = client.New(p.URL, token).Logout(ctx); err != nil {
			return err
		}
	}
	if source == "ACTA_TOKEN" {
		return a.emit(map[string]any{"revoked": true, "credential_source": source}, "Environment-provided session revoked. Unset ACTA_TOKEN to stop using it; saved profile credentials were retained.")
	}
	err = a.repo.Update(func(c *Config) error {
		if c.Profiles[name] != p {
			return errors.New("Profile changed; retry logout")
		}
		pnew := p
		pnew.Credential = ""
		pnew.Storage = ""
		c.Profiles[name] = pnew
		return nil
	})
	if err != nil {
		return err
	}
	if err = a.vault.Remove(p); err != nil {
		a.notice("Session revoked, but the old local credential could not be removed: " + err.Error())
	}
	return a.emit(map[string]any{"profile": name, "authenticated": false}, "Signed out of "+name+".")
}
func (a *App) profileCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "profile", Short: "Manage named server connections"}
	cmd.AddCommand(&cobra.Command{Use: "list", Short: "List profiles and the active selection", Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error {
		c, e := a.repo.Load()
		if e != nil {
			return e
		}
		rows := []map[string]any{}
		lines := []string{}
		for _, n := range names(c) {
			p := c.Profiles[n]
			rows = append(rows, map[string]any{"name": n, "active": n == c.Active, "server": p.URL, "has_saved_credential": p.Credential != ""})
			mark := " "
			if n == c.Active {
				mark = "*"
			}
			lines = append(lines, fmt.Sprintf("%s %s  %s", mark, n, p.URL))
		}
		return a.emit(rows, strings.Join(lines, "\n"))
	}})
	cmd.AddCommand(&cobra.Command{Use: "use <name>", Short: "Set the active profile", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error {
		e := a.repo.Update(func(c *Config) error {
			if _, ok := c.Profiles[args[0]]; !ok {
				return fmt.Errorf("Profile %q does not exist", args[0])
			}
			c.Active = args[0]
			return nil
		})
		if e != nil {
			return e
		}
		return a.emit(map[string]string{"active": args[0]}, "Active profile: "+args[0])
	}})
	var server string
	add := &cobra.Command{Use: "add <name>", Short: "Create a profile without changing the active profile", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error {
		if err := validName(args[0]); err != nil {
			return err
		}
		var err error
		if server != "" {
			server, err = client.NormalizeURL(server)
			if err != nil {
				return err
			}
		}
		err = a.repo.Update(func(c *Config) error {
			if _, ok := c.Profiles[args[0]]; ok {
				return errors.New("Profile already exists")
			}
			c.Profiles[args[0]] = Profile{URL: server}
			return nil
		})
		if err != nil {
			return err
		}
		return a.emit(map[string]string{"profile": args[0], "server": server}, "Created profile "+args[0]+". Sign in with acta -p "+args[0]+" login.")
	}}
	add.Flags().StringVar(&server, "server", "", "Server URL (optional)")
	cmd.AddCommand(add)
	return cmd
}
func openBrowser(ctx context.Context, url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(ctx, "open", url)
	case "windows":
		cmd = exec.CommandContext(ctx, "rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.CommandContext(ctx, "xdg-open", url)
	}
	return cmd.Run()
}

func (a *App) authenticatedClient() (*client.Client, error) {
	_, p, e := a.selected()
	if e != nil {
		return nil, e
	}
	if p.URL == "" {
		return nil, errors.New("No server configured; run acta login <server>")
	}
	token, _, e := a.credential(p)
	if e != nil {
		return nil, e
	}
	if token == "" {
		return nil, errors.New("Not signed in; run acta login")
	}
	return client.New(p.URL, token), nil
}
