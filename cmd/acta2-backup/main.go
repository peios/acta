// acta2-backup runs without the Acta web service or its database. The operator
// provisions its database/repository access and grants the web service only a
// narrow policy/status socket credential.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"acta2/internal/backup"
)

func main() {
	if err := run(); err != nil {
		slog.Error("backup operation failed", "error", err)
		os.Exit(1)
	}
}
func run() error {
	f := flag.NewFlagSet("acta2-backup", flag.ContinueOnError)
	config := f.String("config", "/etc/acta/backup.json", "Operator-owned configuration")
	destination := f.String("destination", "", "Configured recovery destination")
	label := f.String("set", "", "Exact pgBackRest backup set")
	identity := f.String("identity", "", "Private age identity for recovery")
	target := f.String("target", "", "New isolated recovery directory; must not exist")
	targetTime := f.String("time", "", "Optional RFC3339 point-in-time recovery target")
	f.Usage = func() {
		fmt.Fprintln(f.Output(), "Usage: acta2-backup [flags] serve|status|backup|full|drill|list|restore|prepare-cutover")
		f.PrintDefaults()
	}
	if err := f.Parse(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if f.NArg() != 1 {
		return errors.New("usage: acta2-backup [flags] serve|status|backup|full|drill|list|restore|prepare-cutover")
	}
	c, err := backup.LoadConfig(*config)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	op := f.Arg(0)
	if op == "list" {
		if _, ok := c.Destination(*destination); !ok {
			return errors.New("list requires a configured --destination")
		}
		ctx, cancel := context.WithTimeout(ctx, time.Minute)
		defer cancel()
		r, err := (backup.PGBackRest{Config: c}).Catalog(ctx, backup.Policy{Destination: *destination})
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(r)
	}
	if op == "prepare-cutover" {
		ctx, cancel := context.WithTimeout(ctx, time.Duration(c.JobTimeoutMinutes)*time.Minute)
		defer cancel()
		if *target == "" {
			return errors.New("prepare-cutover requires --target pointing to a verified, stopped recovery target")
		}
		r, err := (backup.PGBackRest{Config: c}).PrepareCutover(ctx, *target)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(r)
	}
	if op == "restore" {
		if *label == "" || *identity == "" || *target == "" || *destination == "" {
			return errors.New("restore requires --destination, --set, --identity and --target")
		}
		if _, ok := c.Destination(*destination); !ok {
			return errors.New("unknown destination")
		}
		ctx, cancel := context.WithTimeout(ctx, time.Duration(c.JobTimeoutMinutes)*time.Minute)
		defer cancel()
		r, err := (backup.PGBackRest{Config: c}).Restore(ctx, backup.Policy{Destination: *destination}, *label, *identity, *target, *targetTime)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(r)
	}
	if op == "serve" {
		s, err := backup.Open(c, backup.PGBackRest{Config: c})
		if err != nil {
			return err
		}
		defer s.Close()
		done := make(chan error, 2)
		go func() { done <- s.Run(ctx) }()
		go func() { done <- s.Serve(ctx) }()
		err = <-done
		stop()
		other := <-done
		if err != nil {
			return err
		}
		return other
	}
	client := backup.Client{Socket: c.Socket, TokenFile: c.TokenFile}
	method, path := "GET", "/v1/status"
	var in any
	if op != "status" {
		if op != "backup" && op != "full" && op != "drill" {
			return errors.New("unknown command")
		}
		method, path = "POST", "/v1/jobs"
		in = map[string]string{"kind": op, "actor": "operator"}
	}
	status, raw, err := client.Do(ctx, method, path, in)
	if err != nil {
		return err
	}
	fmt.Println(string(raw))
	if status >= 400 {
		return fmt.Errorf("backup service returned HTTP %d", status)
	}
	return nil
}
