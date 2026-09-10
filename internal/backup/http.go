package backup

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"github.com/gofrs/flock"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

// The Unix socket is a local operator trust boundary; it is not exposed to the
// public network. A remote database deployment can forward this socket through
// an authenticated operator-managed tunnel. There is no arbitrary command or
// repository path parameter in this protocol.
func (s *Service) Serve(ctx context.Context) error {
	socketLock := flock.New(s.c.Socket + ".lock")
	ok, err := socketLock.TryLock()
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("another backup service owns this socket")
	}
	defer socketLock.Unlock()
	raw, err := Secret(s.c.TokenFile)
	if err != nil {
		return err
	}
	token := strings.TrimSpace(string(raw))
	if len(token) < 32 {
		return errors.New("backup API token must contain at least 32 characters")
	}
	if info, err := os.Lstat(s.c.Socket); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return errors.New("backup socket path exists and is not a socket")
		}
		if conn, dialErr := net.DialTimeout("unix", s.c.Socket, time.Second); dialErr == nil {
			conn.Close()
			return errors.New("backup socket already has a listener")
		}
		if err = os.Remove(s.c.Socket); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	l, err := net.Listen("unix", s.c.Socket)
	if err != nil {
		return err
	}
	defer l.Close()
	if err = os.Chmod(s.c.Socket, 0660); err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/status", func(w http.ResponseWriter, r *http.Request) { reply(w, 200, s.View()) })
	mux.HandleFunc("POST /v1/policy", func(w http.ResponseWriter, r *http.Request) {
		var p Policy
		if err := body(r, &p); err != nil {
			replyError(w, 400, "Invalid backup policy.")
			return
		}
		next, err := s.Update(p)
		if err != nil {
			replyOperationError(w, err)
			return
		}
		reply(w, 200, next)
	})
	mux.HandleFunc("POST /v1/jobs", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Kind  string `json:"kind"`
			Actor string `json:"actor"`
		}
		if body(r, &in) != nil || len(in.Actor) > 200 {
			replyError(w, 400, "Invalid backup operation.")
			return
		}
		j, err := s.Enqueue(in.Kind, in.Actor)
		if err != nil {
			replyOperationError(w, err)
			return
		}
		reply(w, 202, j)
	})
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+token)) != 1 {
			replyError(w, 401, "Unauthorized.")
			return
		}
		mux.ServeHTTP(w, r)
	})}
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = server.Close()
		case <-done:
		}
	}()
	err = server.Serve(l)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func body(r *http.Request, v any) error {
	d := json.NewDecoder(io.LimitReader(r.Body, 32<<10))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	var x any
	if d.Decode(&x) != io.EOF {
		return errors.New("expected one JSON body")
	}
	return nil
}
func reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func replyError(w http.ResponseWriter, status int, message string) {
	reply(w, status, map[string]any{"error": map[string]string{"message": message}})
}
func replyOperationError(w http.ResponseWriter, err error) {
	status := 400
	if errors.Is(err, ErrBusy) || errors.Is(err, ErrConflict) {
		status = 409
	}
	replyError(w, status, err.Error())
}

type Client struct {
	Timeout   time.Duration
	Socket    string
	TokenFile string
}

func (c Client) Do(ctx context.Context, method, path string, in any) (int, json.RawMessage, error) {
	if c.Socket == "" || c.TokenFile == "" {
		return 503, nil, errors.New("backup service is not configured")
	}
	raw, err := Secret(c.TokenFile)
	if err != nil {
		return 503, nil, errors.New("backup service credential is unavailable")
	}
	var payload []byte
	if in != nil {
		payload, err = json.Marshal(in)
		if err != nil {
			return 500, nil, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://backup"+path, bytes.NewReader(payload))
	if err != nil {
		return 500, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", c.Socket)
	}}
	defer transport.CloseIdleConnections()
	timeout := c.Timeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	resp, err := (&http.Client{Transport: transport, Timeout: timeout}).Do(req)
	if err != nil {
		return 503, nil, errors.New("backup service is unavailable")
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil || !json.Valid(out) {
		return 502, nil, errors.New("backup service returned an invalid response")
	}
	return resp.StatusCode, out, nil
}
