package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"acta/internal/cli"
	"acta/internal/codehost"
	"acta/internal/codehosts"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var profile, name string
	cmd := &cobra.Command{Use: "acta-code-host", Short: "Connect your private Code host to Acta", Args: cobra.NoArgs, SilenceUsage: true, SilenceErrors: true}
	cmd.Flags().StringVarP(&profile, "profile", "p", "", "Use this Acta profile instead of the active profile")
	cmd.Flags().StringVar(&name, "name", "", "Host display name (defaults to the machine hostname)")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		connection, err := cli.OpenConnection(profile)
		if err != nil {
			return err
		}
		c := connection.Client
		account, err := c.Account(cmd.Context())
		if err != nil {
			return err
		}
		if account.OwnerID != nil {
			return errors.New("Code hosts require a human login; run acta login with your own account")
		}
		if account.MFARequired {
			return errors.New("complete required MFA setup in Acta before starting the host")
		}
		if name == "" {
			name, err = os.Hostname()
			if err != nil {
				return err
			}
		}
		id, unlock, err := codehost.LockIdentity(connection.ConfigDir, c.URL, account.ID)
		if err != nil {
			return err
		}
		defer unlock()
		catalogue, err := codehost.OpenCatalogue(codehost.IdentityDir(connection.ConfigDir, c.URL, account.ID))
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stdout, "Acta Code host · @%s · %s · profile %s\n", account.Username, c.URL, connection.Profile)
		err = codehost.Run(cmd.Context(), c, codehosts.Heartbeat{ID: id, InstanceID: uuid.NewString(), Name: name, OS: runtime.GOOS, Arch: runtime.GOARCH}, catalogue, os.Stdout)
		if err == nil {
			fmt.Fprintln(os.Stdout, "Host stopped.")
		}
		return err
	}
	if err := cmd.ExecuteContext(ctx); err != nil {
		if ctx.Err() != nil {
			return
		}
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
