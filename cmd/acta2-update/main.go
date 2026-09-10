package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"acta2/internal/backup"
	"acta2/internal/localstate"
	"acta2/internal/update"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	cfg := flag.String("config", "/etc/acta-update/config.json", "operator configuration")
	key := flag.String("key", "", "signing seed file (never put in deployment)")
	public := flag.String("public-key", "", "public key output/input file")
	selection := flag.String("release-id", "", "verified available release ID")
	input := flag.String("input", "", "manifest input")
	output := flag.String("output", "", "signed manifest output")
	flag.Parse()
	if flag.NArg() != 1 {
		return errors.New("choose serve, keygen, sign, verify or bootstrap")
	}
	switch flag.Arg(0) {
	case "keygen":
		if *key == "" || *public == "" {
			return errors.New("provide -key and -public-key")
		}
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return err
		}
		for path, raw := range map[string][]byte{*key: priv.Seed(), *public: pub} {
			f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if e != nil {
				return e
			}
			_, e = f.WriteString(base64.StdEncoding.EncodeToString(raw) + "\n")
			f.Close()
			if e != nil {
				return e
			}
		}
		return nil
	case "sign":
		var r update.Release
		if err := localstate.Read(*input, &r); err != nil {
			return err
		}
		raw, err := os.ReadFile(*key)
		if err != nil {
			return err
		}
		var seed []byte
		if _, err = fmt.Sscan(string(raw), new(string)); err != nil {
			return err
		}
		seed, err = base64.StdEncoding.DecodeString(stringTrim(raw))
		if err != nil {
			return err
		}
		signed, err := update.Sign(r, seed)
		if err != nil {
			return err
		}
		return localstate.Write(*output, signed)
	case "verify":
		var s update.Signed
		if err := localstate.Read(*input, &s); err != nil {
			return err
		}
		raw, err := os.ReadFile(*public)
		if err != nil {
			return err
		}
		pub, err := base64.StdEncoding.DecodeString(stringTrim(raw))
		if err != nil {
			return err
		}
		decoded, err := base64.StdEncoding.DecodeString(s.Payload)
		if err != nil {
			return err
		}
		var r update.Release
		if err = json.Unmarshal(decoded, &r); err != nil {
			return err
		}
		r, err = update.Verify(s, pub, r.Repository)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(r)
	}
	c, err := update.LoadConfig(*cfg)
	if err != nil {
		return err
	}
	switch flag.Arg(0) {
	case "status", "check", "install", "retry":
		method, path := "POST", "/v1/"+flag.Arg(0)
		var body any
		if flag.Arg(0) == "status" {
			method = "GET"
		}
		if flag.Arg(0) == "install" {
			body = map[string]string{"id": *selection, "actor": "deployment-operator"}
		}
		status, raw, err := (backup.Client{Socket: c.Socket, TokenFile: c.TokenFile}).Do(context.Background(), method, path, body)
		if err != nil {
			return err
		}
		if status >= 300 {
			return fmt.Errorf("updater returned HTTP %d: %s", status, raw)
		}
		fmt.Println(string(raw))
		return nil
	case "bootstrap":
		if _, err = os.Stat(filepath.Join(c.StateDir, "state.json")); !errors.Is(err, os.ErrNotExist) {
			return errors.New("updater journal already exists or is inaccessible")
		}
		var signed update.Signed
		if err = localstate.Read(*input, &signed); err != nil {
			return err
		}
		pub, e := c.Key()
		if e != nil {
			return e
		}
		r, e := update.Verify(signed, pub, c.Repository)
		if e != nil {
			return e
		}
		// Initial install only. This does not start or replace containers.
		if err = os.MkdirAll(c.StateDir, 0755); err != nil {
			return err
		}
		services := map[string]any{}
		for k, v := range r.Images {
			services[k] = map[string]string{"image": v}
		}
		services["backup"] = map[string]any{"image": r.Images["backup"], "environment": map[string]string{"ACTA_RELEASE": r.Version}}
		if err = localstate.Write(filepath.Join(c.StateDir, "active.json"), map[string]any{"services": services}); err != nil {
			return err
		}
		return localstate.Write(filepath.Join(c.StateDir, "state.json"), update.State{Version: 1, Current: signed, Jobs: []update.Job{}})
	case "serve":
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		d := update.Docker{Config: c}
		s, err := update.Open(c, d)
		if err != nil {
			return err
		}
		defer s.Close()
		done := make(chan struct{})
		go func() { defer close(done); s.Run(ctx) }()
		defer func() { stop(); <-done }()
		return s.Serve(ctx)
	default:
		return errors.New("unknown updater command")
	}
}
func stringTrim(raw []byte) string { var s string; fmt.Sscan(string(raw), &s); return s }
