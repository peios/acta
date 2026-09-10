package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"acta2/internal/accounts"
	"acta2/internal/auth"
	"acta2/internal/config"
	"acta2/internal/httpapi"
	"acta2/internal/postgres"
	"acta2/internal/push"
	"acta2/web"
)

func main() {
	if err := run(); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	listen := flag.String("listen", ":8081", "HTTP listen address")
	verifyRecovery := flag.Bool("verify-recovery", false, "Verify a restored database read-only, without starting the server or applying migrations")
	flag.Parse()
	publicURL := os.Getenv("ACTA_PUBLIC_URL")
	if publicURL == "" {
		publicURL = "http://localhost:8081"
	}
	cfg, err := config.Parse(publicURL, os.Getenv("ACTA_DEV_ORIGIN"))
	if err != nil {
		return err
	}
	cfg.TrustedProxies, err = config.ParseTrustedProxies(os.Getenv("ACTA_TRUSTED_PROXIES"))
	if err != nil {
		return err
	}
	cfg.BackupSocket = os.Getenv("ACTA_BACKUP_SOCKET")
	cfg.BackupTokenFile = os.Getenv("ACTA_BACKUP_TOKEN_FILE")
	cfg.RecoveryMode = os.Getenv("ACTA_RECOVERY_MODE") == "true"
	databaseURL := os.Getenv("ACTA_DATABASE_URL")
	if databaseURL == "" {
		return errors.New("ACTA_DATABASE_URL is required; see learn/development.md")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if *verifyRecovery {
		keyPath := os.Getenv("ACTA_SECURITY_KEY_FILE")
		if keyPath == "" {
			return errors.New("ACTA_SECURITY_KEY_FILE is required for recovery verification")
		}
		if _, err := os.Stat(keyPath); err != nil {
			return err
		}
		key, err := auth.LoadSecurityKey(keyPath)
		if err != nil {
			return err
		}
		if err = postgres.VerifyRecovery(ctx, databaseURL, key); err != nil {
			return err
		}
		if _, err = web.Handler(); err != nil {
			return err
		}
		fmt.Println("Restored schema, security data, documents and embedded UI verified.")
		return nil
	}
	// Projection backfill is cancellable with server shutdown, but must not be
	// repeatedly rolled back by the short authentication-startup deadline.
	store, err := postgres.Open(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer store.Close()
	startup, cancelStartup := context.WithTimeout(ctx, 30*time.Second)
	defer cancelStartup()
	service, code, err := auth.New(startup, store)
	if err != nil {
		return err
	}
	if code != "" {
		fmt.Fprintf(os.Stderr, "\nSet up Acta at %s/setup\nOne-time setup code (valid for one hour):\n%s\n\n", cfg.PublicURL, code)
	}

	keyPath := os.Getenv("ACTA_SECURITY_KEY_FILE")
	if keyPath == "" {
		keyPath = ".local/security.key"
	}
	key, err := auth.LoadSecurityKey(keyPath)
	if err != nil {
		return err
	}
	origins := []string{}
	for origin := range cfg.Origins {
		origins = append(origins, origin)
	}
	security, err := auth.NewSecurity(startup, service, store, cfg.PublicURL, origins, key)
	if err != nil {
		return err
	}
	privatePush, publicPush, err := push.VAPID(key)
	if err != nil {
		return err
	}
	pushDone := make(chan struct{})
	go func() {
		defer close(pushDone)
		if !cfg.RecoveryMode {
			push.Run(ctx, store, push.Sender{Private: privatePush, Public: publicPush, Contact: cfg.PublicURL, Client: push.HTTPClient()})
		}
	}()
	defer func() { stop(); <-pushDone }()
	handler, err := web.Handler()
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	api := httpapi.New(service, security, accounts.NewProfileService(store), auth.NewManagement(service, security, store), cfg, publicPush)
	for _, path := range []string{"/api/", "/oauth/", "/.well-known/", "/mcp"} {
		mux.Handle(path, api)
	}
	mux.Handle("/", handler)
	secured := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("X-Frame-Options", "DENY")
		// SvelteKit embeds the resource policy with hashes for its bootstrap script.
		// Frame restrictions must be a response header, not an HTML meta policy.
		w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'")
		mux.ServeHTTP(w, r)
	})
	server := &http.Server{
		Addr:              *listen,
		BaseContext:       func(net.Listener) context.Context { return ctx },
		Handler:           secured,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	cleanupDone := make(chan struct{})
	defer func() { stop(); <-cleanupDone }()
	go func() {
		defer close(cleanupDone)
		ticker := time.NewTicker(15 * time.Minute)
		defer ticker.Stop()
		for {
			cleanupCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
			if err := service.Cleanup(cleanupCtx); err != nil && ctx.Err() == nil {
				slog.Error("authentication cleanup failed", "error", err)
			}
			cancel()
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	stopped := make(chan error, 1)
	go func() {
		slog.Info("Acta listening", "address", *listen)
		stopped <- server.ListenAndServe()
	}()

	select {
	case err := <-stopped:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
			return err
		}
		if err := <-stopped; !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}
