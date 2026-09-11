package cli

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"time"

	"acta/internal/client"
	"charm.land/huh/v2"
	"github.com/spf13/cobra"
)

func (a *App) loginCommand() *cobra.Command {
	var noBrowser, file, replace, changeServer bool
	var newProfile string
	cmd := &cobra.Command{Use: "login [server]", Short: "Sign in through your browser", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		if os.Getenv("ACTA_TOKEN") != "" {
			return errors.New("ACTA_TOKEN is supplying authentication. Unset it before saving a browser login")
		}
		name, old, err := a.selected()
		if err != nil {
			return err
		}
		initialURL := old.URL
		creating := false
		if newProfile != "" {
			name = newProfile
			old = Profile{}
			creating = true
		} else if old.Credential != "" && !replace {
			if !a.interactive {
				return errors.New("Profile already has a login; use --replace or --new-profile <name>")
			}
			choice := "new"
			if err = a.prompt(ctx, huh.NewSelect[string]().Title("This profile already has a login").Options(huh.NewOption("Create a new profile", "new"), huh.NewOption("Replace this profile's login", "replace"), huh.NewOption("Cancel", "cancel")).Value(&choice)); err != nil {
				return err
			}
			if choice == "cancel" {
				return errors.New("Login cancelled")
			}
			if choice == "new" {
				name = ""
				if err = a.prompt(ctx, huh.NewInput().Title("New profile name").Value(&name).Validate(validName)); err != nil {
					return err
				}
				old = Profile{}
				creating = true
			}
		}
		if err = validName(name); err != nil {
			return err
		}
		snapshot, err := a.repo.Load()
		if err != nil {
			return err
		}
		if _, exists := snapshot.Profiles[name]; creating && exists {
			return fmt.Errorf("Profile %q already exists", name)
		}
		server := old.URL
		if creating {
			server = initialURL
		}
		if len(args) > 0 {
			server, err = client.NormalizeURL(args[0])
			if err != nil {
				return err
			}
		}
		if server == "" {
			if !a.interactive {
				return errors.New("No server configured; supply acta login <server>")
			}
			if err = a.prompt(ctx, huh.NewInput().Title("Acta server URL").Placeholder("https://acta.example.org").Value(&server).Validate(func(v string) error { _, e := client.NormalizeURL(v); return e })); err != nil {
				return err
			}
		}
		server, err = client.NormalizeURL(server)
		if err != nil {
			return err
		}
		if old.URL != "" && old.URL != server && !changeServer {
			if !a.interactive {
				return errors.New("Server differs from the saved profile; use --change-server to confirm")
			}
			ok, e := a.confirm(ctx, fmt.Sprintf("Profile %q is configured for %s. Change it to %s?", name, old.URL, server))
			if e != nil {
				return e
			}
			if !ok {
				return errors.New("Login cancelled")
			}
		}
		c := client.New(server, "")
		machine, _ := os.Hostname()
		d, err := c.Start(ctx, machine)
		if err != nil {
			return err
		}
		link := server + "/login/device?code=" + url.QueryEscape(d.UserCode)
		a.notice(fmt.Sprintf("Open %s/login/device and enter %s\nOr visit %s\nWaiting for browser approval…", server, d.UserCode, link))
		if !noBrowser {
			openCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			e := openBrowser(openCtx, link)
			cancel()
			if e != nil {
				a.notice("Could not open a browser automatically. Use the URL and code above.")
			}
		}
		secret, err := c.Wait(ctx, d)
		if err != nil {
			return err
		}
		c.Token = secret
		// A received session is revoked if local persistence fails. Do not leave an
		// unnoticed usable session behind after an unsuccessful login command.
		saved := false
		defer func() {
			if !saved {
				cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if e := c.Logout(cleanup); e != nil {
					a.notice("Could not revoke the unfinished CLI login; revoke it in User Settings → Security.")
				}
			}
		}()
		account, err := c.Account(ctx)
		if err != nil {
			return err
		}
		fresh, notice, err := a.vault.Save(secret, file)
		if err != nil {
			return err
		}
		fresh.URL = server
		err = a.repo.Update(func(config *Config) error {
			current, exists := config.Profiles[name]
			if (creating && exists) || (!creating && (!exists || current != old)) {
				return errors.New("Profile changed during login; its saved settings were preserved")
			}
			config.Profiles[name] = fresh
			return nil
		})
		if err != nil {
			_ = a.vault.Remove(fresh)
			return err
		}
		saved = true
		if notice != "" {
			a.notice(notice)
		}
		if old.Credential != "" {
			// Replacement revokes only the previous profile session, never another
			// profile's session or the browser used to approve this login.
			oldToken, e := a.vault.Read(old)
			if e == nil && oldToken != "" {
				cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				e = client.New(old.URL, oldToken).Logout(cleanup)
				cancel()
			}
			if e != nil {
				a.notice("The previous session could not be revoked; it can be removed in that server's Security page.")
			}
			if e = a.vault.Remove(old); e != nil {
				a.notice("The previous local credential could not be removed: " + e.Error())
			}
		}
		if creating && a.interactive {
			ok, e := a.confirm(ctx, "Make "+name+" the active profile?")
			if e == nil && ok {
				if e = a.repo.Update(func(c *Config) error { c.Active = name; return nil }); e != nil {
					a.notice("Signed in, but could not change active profile: " + e.Error())
				}
			}
		}
		return a.emit(map[string]any{"profile": name, "server": server, "account": account, "credential_source": fresh.Storage}, fmt.Sprintf("Signed in as @%s on %s (profile %s).", account.Username, server, name))
	}}
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "Print the browser URL without opening it")
	cmd.Flags().BoolVar(&file, "insecure-storage", false, "Store credentials in an owner-only plaintext file")
	cmd.Flags().BoolVar(&replace, "replace", false, "Replace this profile's existing login")
	cmd.Flags().BoolVar(&changeServer, "change-server", false, "Confirm changing the saved server URL")
	cmd.Flags().StringVar(&newProfile, "new-profile", "", "Save login as a new profile")
	cmd.MarkFlagsMutuallyExclusive("replace", "new-profile")
	return cmd
}
