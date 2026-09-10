package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	"acta2/internal/hyperharness"
	"acta2/internal/providers"
	"acta2/internal/threads"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"reflect"
)

type HarnessState struct {
	State    string  `json:"state"`
	Hostname string  `json:"hostname"`
	RetryIn  float64 `json:"retry_in,omitempty"`
}

// RunHarness owns only the server connection. Future provider/process ownership
// must remain independent of this retry loop.
func (c *Client) RunHarness(ctx context.Context, hostname string, discovery providers.Source, runtime *hyperharness.Controller, report func(HarnessState) error) error {
	if !hyperharness.ValidHostname(hostname) {
		return errors.New("This machine's hostname is empty or invalid")
	}
	server, e := NormalizeURL(c.URL)
	if e != nil {
		return e
	}
	delay := time.Second
	for {
		if ctx.Err() != nil {
			return nil
		}
		started := time.Now()
		err := c.harnessConnection(ctx, server, hostname, discovery, runtime, report)
		if ctx.Err() != nil {
			return nil
		}
		if runtime != nil && runtime.Err() != nil {
			return runtime.Err()
		}
		var apiErr *Error
		if errors.As(err, &apiErr) && apiErr.Status >= 400 && apiErr.Status < 500 && apiErr.Status != 429 {
			if apiErr.Status == 401 {
				return fmt.Errorf("harness authentication ended; run acta2 login using the selected profile: %w", err)
			}
			return err
		}
		var outputErr *harnessOutputError
		if errors.As(err, &outputErr) {
			return outputErr.err
		}
		if time.Since(started) >= 2*hyperharness.Heartbeat {
			delay = time.Second
		}
		if e := waitHarnessRetry(ctx, hostname, delay, report); e != nil {
			if ctx.Err() != nil {
				return nil
			}
			return e
		}
		delay = min(delay*2, 30*time.Second)
	}
}

type harnessOutputError struct{ err error }

func (e *harnessOutputError) Error() string { return e.err.Error() }

func (c *Client) harnessConnection(ctx context.Context, server, hostname string, discovery providers.Source, runtime *hyperharness.Controller, report func(HarnessState) error) error {
	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	// Preserve the client's transport (including test transports), but explicitly
	// prevent redirects from forwarding the profile credential to another host.
	hc := *c.HTTP
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	conn, resp, err := websocket.Dial(dialCtx, server+"/api/harnesses/connect", &websocket.DialOptions{HTTPClient: &hc, HTTPHeader: http.Header{"Authorization": []string{"Bearer " + c.Token}, "User-Agent": []string{"acta2-harness"}}, Subprotocols: []string{hyperharness.Protocol}})
	cancel()
	if err != nil {
		if resp != nil && resp.StatusCode != http.StatusSwitchingProtocols {
			var result struct{ Error Error }
			if resp.Body != nil {
				_ = json.NewDecoder(resp.Body).Decode(&result)
			}
			result.Error.Status = resp.StatusCode
			if result.Error.Message == "" {
				result.Error.Message = fmt.Sprintf("Acta returned HTTP %d while connecting the harness", resp.StatusCode)
			}
			// Redirects must fail instead of looping forever against the wrong address.
			if resp.StatusCode >= 300 && resp.StatusCode < 400 {
				result.Error.Status = 400
			}
			return &result.Error
		}
		return err
	}
	defer conn.CloseNow()
	if conn.Subprotocol() != hyperharness.Protocol {
		return &Error{Status: 426, Message: "Unsupported harness protocol. Update the CLI and server together."}
	}
	conn.SetReadLimit(threads.MaxMessageWire)
	writeCtx, cancel := context.WithTimeout(ctx, hyperharness.ResponseTimeout)
	err = wsjson.Write(writeCtx, conn, hyperharness.Hello{Hostname: hostname})
	cancel()
	if err != nil {
		return err
	}
	// Discovery writes run independently from reads so probes and writes cannot
	// prevent pong processing. Reconnects always send the latest full snapshot.
	if discovery != nil {
		streamCtx, stop := context.WithCancel(ctx)
		ctx = streamCtx
		done := make(chan struct{})
		go func() { defer close(done); defer stop(); sendProviderUpdates(ctx, conn, discovery, runtime) }()
		defer func() { stop(); conn.CloseNow(); <-done }()
	}
	connected := false
	for {
		timeout := hyperharness.ResponseTimeout
		if connected {
			timeout = 2*hyperharness.Heartbeat + hyperharness.ResponseTimeout
		}
		readCtx, cancel := context.WithTimeout(ctx, timeout)
		var message struct {
			Type       string                  `json:"type"`
			Code       string                  `json:"code"`
			Message    string                  `json:"message"`
			Control    threads.Control         `json:"control"`
			ThreadID   string                  `json:"thread_id"`
			Sequence   int64                   `json:"sequence"`
			Connection hyperharness.Connection `json:"connection"`
		}
		err = wsjson.Read(readCtx, conn, &message)
		cancel()
		if err != nil {
			if websocket.CloseStatus(err) == websocket.StatusPolicyViolation {
				return &Error{Status: 403, Message: "Acta rejected this harness connection. Check the selected account and CLI version."}
			}
			return err
		}
		switch message.Type {
		case "control":
			if runtime == nil {
				return &Error{Status: 426, Message: "Thread runtime unavailable"}
			}
			if err := runtime.Control(message.Control); err != nil {
				if runtime.Err() != nil {
					return runtime.Err()
				}
				if (message.Control.Action == "approval" || message.Control.Action == "answer") && errors.Is(err, hyperharness.ErrBusy) {
					continue
				}
				outcome := ""
				if message.Control.Action == "send" || message.Control.Action == "configure" || message.Control.Action == "models" || message.Control.Action == "permissions" || message.Control.Action == "approval" || message.Control.Action == "answer" {
					outcome = "rejected"
				}
				if err = writeHarnessMessage(ctx, conn, hyperharness.ProviderUpdate{Type: "result", Result: &threads.Result{ID: message.Control.ID, ThreadID: message.Control.ThreadID, Error: err.Error(), Outcome: outcome}}); err != nil {
					return err
				}
			}
		case "frame_ack":
			if runtime != nil {
				if err := runtime.Acknowledge(message.ThreadID, message.Sequence); err != nil {
					return err
				}
			}
		case "connected":
			if connected || message.Connection.ID == "" || message.Connection.Hostname != hostname {
				return &Error{Status: 426, Message: "Invalid harness connection acknowledgement"}
			}
			connected = true
			if err := report(HarnessState{State: "connected", Hostname: hostname}); err != nil {
				return &harnessOutputError{err}
			}
		case "heartbeat":
			if !connected {
				return &Error{Status: 426, Message: "Missing harness connection acknowledgement"}
			}
		case "error":
			status := 403
			switch message.Code {
			case "unauthenticated":
				status = 401
			case "unavailable", "busy":
				status = 503
			}
			return &Error{Status: status, Code: message.Code, Message: message.Message}
		default:
			return &Error{Status: 426, Message: "Unknown harness protocol message. Update the CLI and server together."}
		}
	}
}

func sendProviderUpdates(ctx context.Context, c *websocket.Conn, source providers.Source, runtime *hyperharness.Controller) {
	changes, unsubscribe := source.Subscribe()
	defer unsubscribe()
	var last []providers.Status
	var inventory []threads.Descriptor
	results := map[string]threads.Result{}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		states := source.Snapshot()
		if !slices.Equal(states, last) {
			write, cancel := context.WithTimeout(ctx, hyperharness.ResponseTimeout)
			err := wsjson.Write(write, c, hyperharness.ProviderUpdate{Type: "providers", Providers: states})
			cancel()
			if err != nil {
				return
			}
			last = states
		}
		if runtime != nil {
			current, err := runtime.Inventory(ctx)
			if err != nil {
				return
			}
			if !reflect.DeepEqual(current, inventory) {
				update := hyperharness.ProviderUpdate{Type: "inventory", Threads: current}
				for _, t := range current {
					for _, prior := range inventory {
						if prior.ID == t.ID && !prior.Committed && t.Committed {
							update.Type = "committed"
							update.ThreadID = t.ID
						}
					}
				}
				if writeHarnessMessage(ctx, c, update) != nil {
					return
				}
				inventory = current
			}
			for _, t := range current {
				frames, err := runtime.Frames(ctx, t.ID)
				if err != nil {
					return
				}
				if len(frames) > 0 {
					if writeHarnessMessage(ctx, c, hyperharness.ProviderUpdate{Type: "frames", ThreadID: t.ID, Frames: frames}) != nil {
						return
					}
				}
			}
			for _, result := range runtime.Results() {
				if prior, ok := results[result.ID]; !ok || !reflect.DeepEqual(prior, result) {
					if writeHarnessMessage(ctx, c, hyperharness.ProviderUpdate{Type: "result", Result: &result}) != nil {
						return
					}
					results[result.ID] = result
				}
			}
		}

		select {
		case <-ctx.Done():
			return
		case <-changes:
		case <-ticker.C:
		}
	}
}

func writeHarnessMessage(ctx context.Context, c *websocket.Conn, value any) error {
	write, cancel := context.WithTimeout(ctx, hyperharness.ResponseTimeout)
	defer cancel()
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return err
	}
	return c.Write(write, websocket.MessageText, encoded.Bytes())
}
