package update

import (
	"acta/internal/backup"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"time"
)

func (s *Service) Serve(ctx context.Context) error {
	raw, err := backup.Secret(s.c.TokenFile)
	if err != nil {
		return err
	}
	token := stringTrim(raw)
	if len(token) < 32 {
		return errors.New("updater token must have at least 32 characters")
	}
	if info, err := os.Lstat(s.c.Socket); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return errors.New("updater socket path is not a socket")
		}
		if c, e := net.DialTimeout("unix", s.c.Socket, time.Second); e == nil {
			c.Close()
			return errors.New("updater socket already has a listener")
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
	if err = os.Chown(s.c.Socket, -1, 10001); err != nil {
		return err
	}
	mux := http.NewServeMux()
	respond := func(w http.ResponseWriter, status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	fail := func(w http.ResponseWriter, err error) {
		code := 400
		if errors.Is(err, ErrBusy) {
			code = 409
		}
		respond(w, code, map[string]any{"error": map[string]string{"message": err.Error()}})
	}
	mux.HandleFunc("GET /v1/status", func(w http.ResponseWriter, r *http.Request) {
		st := s.View()
		current, _ := Verify(st.Current, s.key, s.c.Repository)
		var available any
		if st.Available != nil {
			release, e := Verify(*st.Available, s.key, s.c.Repository)
			if e == nil {
				reason := ""
				if e = Compatible(current, release); e != nil {
					reason = e.Error()
				}
				available = map[string]any{"id": st.Available.ID(), "release": release, "blocked": reason}
			}
		}
		jobs := []map[string]any{}
		for _, j := range st.Jobs {
			release, _ := Verify(j.Target, s.key, s.c.Repository)
			jobs = append(jobs, map[string]any{"id": j.ID, "version": release.Version, "phase": j.Phase, "error": j.Error, "paused": j.Paused, "started_at": j.Started, "updated_at": j.Updated})
			if len(jobs) >= 20 {
				break
			}
		}
		respond(w, 200, map[string]any{"configured": true, "repository": s.c.Repository, "current": current, "available": available, "checked_at": st.Checked, "check_error": st.CheckError, "jobs": jobs})
	})
	mux.HandleFunc("POST /v1/check", func(w http.ResponseWriter, r *http.Request) {
		s.RequestCheck()
		respond(w, 202, map[string]bool{"checking": true})
	})
	mux.HandleFunc("POST /v1/install", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			ID    string `json:"id"`
			Actor string `json:"actor"`
		}
		decoder := json.NewDecoder(io.LimitReader(r.Body, 4097))
		decoder.DisallowUnknownFields()
		var extra any
		if decoder.Decode(&in) != nil || decoder.Decode(&extra) != io.EOF || len(in.Actor) > 200 {
			respond(w, 400, map[string]string{"error": "invalid install request"})
			return
		}
		j, err := s.Install(in.ID, in.Actor)
		if err != nil {
			fail(w, err)
			return
		}
		respond(w, 202, map[string]string{"id": j.ID})
	})
	mux.HandleFunc("POST /v1/retry", func(w http.ResponseWriter, r *http.Request) {
		if err := s.Retry(); err != nil {
			fail(w, err)
			return
		}
		respond(w, 202, map[string]bool{"queued": true})
	})
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 120 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+token)) != 1 {
			respond(w, 401, map[string]string{"error": "unauthorized"})
			return
		}
		mux.ServeHTTP(w, r)
	})}
	go func() { <-ctx.Done(); _ = server.Close() }()
	err = server.Serve(l)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
