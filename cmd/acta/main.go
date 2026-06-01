// Command acta is the Acta server and its admin CLI.
//
//	acta serve                          run the HTTP server (default)
//	acta createuser -username <name>    create a local account
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/peios/acta/internal/authn/local"
	"github.com/peios/acta/internal/config"
	"github.com/peios/acta/internal/passkey"
	"github.com/peios/acta/internal/session"
	"github.com/peios/acta/internal/store"
	"github.com/peios/acta/internal/store/postgres"
	"github.com/peios/acta/internal/web"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))

	args := os.Args[1:]
	cmd := "serve"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, args = args[0], args[1:]
	}

	var err error
	switch cmd {
	case "serve":
		err = runServe(args)
	case "createuser":
		err = runCreateUser(args)
	default:
		err = fmt.Errorf("unknown command %q (want: serve, createuser)", cmd)
	}
	if err != nil {
		slog.Error(cmd, "err", err)
		os.Exit(1)
	}
}

func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg := config.Load()

	pg, err := openAndMigrate(context.Background(), cfg)
	if err != nil {
		return err
	}
	defer pg.Close()

	if err := maybeBootstrap(context.Background(), pg); err != nil {
		return fmt.Errorf("bootstrap: %w", err)
	}

	sessions := session.New(pg, session.Config{
		Secure:          cfg.CookieSecure(),
		IdleTimeout:     cfg.SessionIdle,
		AbsoluteTimeout: cfg.SessionAbsolute,
	})
	passkeys, err := passkey.New(pg, passkey.Config{
		RPID:     cfg.RPID,
		RPOrigin: cfg.RPOrigin,
		RPName:   cfg.RPName,
	})
	if err != nil {
		return fmt.Errorf("passkey: %w", err)
	}
	provider := local.NewProvider(pg, sessions, passkeys, cfg.CookieSecure())
	handler := web.NewHandler(cfg, sessions, provider, passkeys)

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	shutdownCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", cfg.HTTPAddr, "env", cfg.Env)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-shutdownCtx.Done():
		slog.Info("shutting down")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(ctx)
	}
}

func runCreateUser(args []string) error {
	fs := flag.NewFlagSet("createuser", flag.ContinueOnError)
	username := fs.String("username", "", "username (required)")
	display := fs.String("display", "", "display name (defaults to username)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *username == "" {
		return errors.New("-username is required")
	}
	uname := local.NormalizeUsername(*username)
	disp := *display
	if disp == "" {
		disp = *username
	}

	password, err := readPassword()
	if err != nil {
		return err
	}
	if password == "" {
		return errors.New("password must not be empty")
	}
	hash, err := local.HashPassword(password)
	if err != nil {
		return err
	}

	cfg := config.Load()
	pg, err := openAndMigrate(context.Background(), cfg)
	if err != nil {
		return err
	}
	defer pg.Close()

	u, err := pg.CreateUser(context.Background(), store.NewUser{
		Username: uname, Display: disp, PasswordHash: hash,
	})
	if errors.Is(err, store.ErrUsernameTaken) {
		return fmt.Errorf("username %q already exists", uname)
	}
	if err != nil {
		return err
	}
	fmt.Printf("created user %s (%s)\n", u.Username, u.ID)
	return nil
}

// maybeBootstrap creates a first admin account from ACTA_BOOTSTRAP_USERNAME /
// ACTA_BOOTSTRAP_PASSWORD when both are set, so a freshly-hosted instance has
// something to log in as. It's idempotent: if the account already exists (or
// is created concurrently by another instance) it does nothing.
func maybeBootstrap(ctx context.Context, st store.Store) error {
	username := os.Getenv("ACTA_BOOTSTRAP_USERNAME")
	password := os.Getenv("ACTA_BOOTSTRAP_PASSWORD")
	if username == "" || password == "" {
		return nil
	}
	uname := local.NormalizeUsername(username)

	switch _, err := st.UserByUsername(ctx, uname); {
	case err == nil:
		return nil // already present
	case errors.Is(err, store.ErrUserNotFound):
		// fall through and create
	default:
		return err
	}

	hash, err := local.HashPassword(password)
	if err != nil {
		return err
	}
	u, err := st.CreateUser(ctx, store.NewUser{Username: uname, Display: username, PasswordHash: hash})
	if errors.Is(err, store.ErrUsernameTaken) {
		return nil // lost a race with another instance; fine
	}
	if err != nil {
		return err
	}
	slog.Info("bootstrap admin created", "username", u.Username)
	return nil
}

func openAndMigrate(ctx context.Context, cfg config.Config) (*postgres.Postgres, error) {
	pg, err := postgres.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	if err := pg.Migrate(ctx); err != nil {
		pg.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return pg, nil
}

// readPassword prefers the ACTA_SEED_PASSWORD env var (keeps the secret out of
// argv and shell history); otherwise it reads a single line from stdin.
func readPassword() (string, error) {
	if p := os.Getenv("ACTA_SEED_PASSWORD"); p != "" {
		return p, nil
	}
	fmt.Fprint(os.Stderr, "Password: ")
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}
