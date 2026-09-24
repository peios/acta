package codehost

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"time"

	"acta/internal/client"
	"acta/internal/codeagents"
	"acta/internal/codehosts"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/google/uuid"
)

// Run maintains the native connection. Each connection gets a fresh instance
// ID, so a late disconnect/release cannot invalidate a replacement connection.
func Run(ctx context.Context, c *client.Client, in codehosts.Heartbeat, catalogue *Catalogue, out io.Writer) error {
	if err := in.Validate(); err != nil {
		return err
	}
	if catalogue == nil {
		return errors.New("codebase catalogue is required")
	}
	threads := NewThreads(ctx, catalogue, map[string]codeagents.Adapter{"claude": codeagents.Claude{}, "codex": codeagents.Codex{}})
	defer threads.Close()
	slots := make(chan struct{}, 4)
	retry := time.Second
	reported := false
	for {
		if ctx.Err() != nil {
			return nil
		}
		in.InstanceID = uuid.NewString()
		hello, cancel := context.WithTimeout(ctx, 5*time.Second)
		conn, err := c.ConnectCodeHost(hello, in)
		cancel()
		if err == nil {
			fmt.Fprintf(out, "Connected: %s (%s)\n", in.Name, in.ID)
			retry = time.Second
			reported = false
			err = serve(ctx, conn, threads, slots)
			conn.CloseNow()
		}
		if ctx.Err() != nil {
			return nil
		}
		var apiError *client.Error
		var protocol *codehosts.Problem
		if errors.As(err, &apiError) && apiError.Status >= 400 && apiError.Status < 500 && apiError.Status != 429 {
			return err
		}
		if errors.As(err, &protocol) && protocol.Code != "host_in_use" && protocol.Code != "unavailable" && protocol.Code != "busy" {
			return err
		}
		if !reported {
			fmt.Fprintf(out, "Connection unavailable: %v. Retrying.\n", err)
			reported = true
		}
		delay := retry + time.Duration(rand.Int64N(int64(retry/2)))
		retry = min(retry*2, 10*time.Second)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}

func serve(parent context.Context, conn *websocket.Conn, threads *Threads, slots chan struct{}) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	discoveryDone := make(chan struct{})
	go func() { defer close(discoveryDone); publishProviders(ctx, conn, discoverProviders) }()
	defer func() { cancel(); <-discoveryDone }()
	for {
		var request codehosts.Request
		if err := wsjson.Read(ctx, conn, &request); err != nil {
			return err
		}
		select {
		case slots <- struct{}{}:
			go func() {
				defer func() { <-slots }()
				work, stop := context.WithTimeout(ctx, 8*time.Second)
				defer stop()
				response := threads.Handle(work, request)
				_ = wsjson.Write(work, conn, response)
			}()
		default:
			write, stop := context.WithTimeout(ctx, time.Second)
			err := wsjson.Write(write, conn, codehosts.Response{ID: request.ID, Error: &codehosts.Problem{Code: "host_busy", Message: "The host is busy. Try again shortly."}})
			stop()
			if err != nil {
				return err
			}
		}
	}
}
