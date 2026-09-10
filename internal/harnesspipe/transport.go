package harnesspipe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"time"
)

// Serve exposes only the engine over a private local socket. No Acta credential
// or provider-specific parsing lives in this process.
func Serve(ctx context.Context, dir string) (result error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	engine, err := Open(dir)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, engine.Close()) }()
	socket := filepath.Join(dir, "pipe.sock")
	if err = os.Remove(socket); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		return err
	}
	defer listener.Close()
	defer os.Remove(socket)
	if err = os.Chmod(socket, 0600); err != nil {
		return err
	}
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, Handler: Handler(engine)}
	done := make(chan struct{})
	go func() { defer close(done); <-ctx.Done(); server.Close() }()
	err = server.Serve(listener)
	if ctx.Err() != nil {
		<-done
		return nil
	}
	return err
}
func Handler(api API) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Acta-Pipe-Protocol", "1")
		w.Header().Set("X-Acta-Pipe-Max-Input", strconv.Itoa(MaxInput))
		if r.Method != "POST" {
			http.Error(w, "method not allowed", 405)
			return
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2*MaxInput))
		decoder.DisallowUnknownFields()
		var value any
		var err error
		switch r.URL.Path {
		case "/spawn":
			var p Spec
			err = decoder.Decode(&p)
			if err == nil {
				value, err = api.Spawn(r.Context(), p)
			}
		case "/write":
			var p WriteRequest
			err = decoder.Decode(&p)
			if err == nil {
				err = api.Write(r.Context(), p)
			}
		case "/kill":
			var p struct {
				RunID string `json:"run_id"`
			}
			err = decoder.Decode(&p)
			if err == nil {
				err = api.Kill(r.Context(), p.RunID)
			}
		case "/processes":
			value, err = api.Processes(r.Context())
		case "/read":
			var p ReadRequest
			err = decoder.Decode(&p)
			if err == nil {
				value, err = api.Read(r.Context(), p)
			}
		default:
			http.Error(w, "not found", 404)
			return
		}
		if err != nil {
			w.WriteHeader(409)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		encoder := json.NewEncoder(w)
		encoder.SetEscapeHTML(false)
		_ = encoder.Encode(value)
	})
}

type Remote struct {
	maxInput  atomic.Int64
	client    *http.Client
	transport *http.Transport
}

func Connect(socket string) *Remote {
	tr := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	return &Remote{client: &http.Client{Transport: tr, Timeout: 15 * time.Second}, transport: tr}
}
func (r *Remote) Close() { r.transport.CloseIdleConnections() }
func (r *Remote) call(ctx context.Context, path string, in, out any) error {
	raw, err := json.Marshal(in)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", "http://pipe"+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.Header.Get("X-Acta-Pipe-Protocol") != "1" {
		return errors.New("incompatible detached pipe; restart it with the current CLI")
	}
	limit, err := strconv.Atoi(resp.Header.Get("X-Acta-Pipe-Max-Input"))
	if err != nil || limit <= 0 {
		limit = 1024 * 1024
	} // Original detached processes have the 1 MiB bound.
	r.maxInput.Store(int64(min(limit, MaxInput)))
	decoder := json.NewDecoder(io.LimitReader(resp.Body, MaxRecord))
	if resp.StatusCode != 200 {
		var body struct{ Error string }
		if decoder.Decode(&body) != nil {
			return errors.New("invalid pipe response")
		}
		return errors.New(body.Error)
	}
	if out == nil {
		_, err = io.Copy(io.Discard, resp.Body)
		return err
	}
	return decoder.Decode(out)
}
func (r *Remote) Spawn(ctx context.Context, s Spec) (p Process, err error) {
	err = r.call(ctx, "/spawn", s, &p)
	return
}
func (r *Remote) Write(ctx context.Context, w WriteRequest) error {
	return r.call(ctx, "/write", w, nil)
}
func (r *Remote) Kill(ctx context.Context, id string) error {
	return r.call(ctx, "/kill", map[string]string{"run_id": id}, nil)
}
func (r *Remote) Processes(ctx context.Context) (p []Process, err error) {
	err = r.call(ctx, "/processes", nil, &p)
	return
}
func (r *Remote) Read(ctx context.Context, q ReadRequest) (f []RawFrame, err error) {
	err = r.call(ctx, "/read", q, &f)
	return
}

// Refresh the live process capability before a new image submission. A known
// undersized pipe can reject before any provider input is attempted.
func (r *Remote) InputLimit(ctx context.Context) (int, error) {
	if _, err := r.Processes(ctx); err != nil {
		return 0, err
	}
	return int(r.maxInput.Load()), nil
}
