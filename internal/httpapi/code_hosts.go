package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"acta/internal/auth"
	"acta/internal/codebases"
	"acta/internal/codehosts"
	"acta/internal/codethreads"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func (h *Handler) codeHostRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/code/hosts", h.listCodeHosts)
	mux.HandleFunc("GET /api/code/hosts/connect", h.connectCodeHost)
	mux.HandleFunc("POST /api/code/hosts/{id}/request", h.codeHostRequest)
}
func (h *Handler) listCodeHosts(w http.ResponseWriter, r *http.Request) {
	secret := token(r, h.sessionCookie)
	owner, err := h.auth.CodeHostOwner(r.Context(), secret, false)
	if err != nil {
		failure(w, err)
		return
	}
	hosts, err := h.auth.CodeHosts(r.Context(), secret)
	if err != nil {
		failure(w, err)
		return
	}
	for i := range hosts {
		peer := h.codeHosts.Get(owner, hosts[i].ID)
		hosts[i].Connected = hosts[i].Online && peer != nil
		if hosts[i].Connected {
			hosts[i].ProviderStatus = peer.ProviderStatus(time.Now())
		}
	}
	writeJSON(w, 200, map[string]any{"hosts": hosts})
}

func (h *Handler) connectCodeHost(w http.ResponseWriter, r *http.Request) {
	// Native-only, never browser-cookie authority or cross-origin WebSockets.
	if r.Header.Get("Origin") != "" || !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer cli_") {
		failure(w, auth.ErrForbidden)
		return
	}
	secret := token(r, h.sessionCookie)
	check, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	owner, err := h.auth.CodeHostOwner(check, secret, true)
	cancel()
	if err != nil {
		failure(w, err)
		return
	}
	control := http.NewResponseController(w)
	_ = control.SetReadDeadline(time.Time{})
	_ = control.SetWriteDeadline(time.Time{})
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(codehosts.MaxWireBytes)
	ctx, stop := context.WithCancel(r.Context())
	defer stop()
	hello, cancel := context.WithTimeout(ctx, 5*time.Second)
	var in codehosts.Heartbeat
	err = wsjson.Read(hello, conn, &in)
	if err == nil {
		_, err = h.auth.RenewCodeHost(hello, secret, in)
	}
	if err != nil {
		_, p := classifyError(err)
		_ = wsjson.Write(hello, conn, codehosts.Response{Error: &codehosts.Problem{Code: p.Code, Message: p.Message}})
		cancel()
		return
	}
	cancel()
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = h.auth.ReleaseCodeHost(cleanup, secret, in.ID, in.InstanceID)
	}()
	peer := codehosts.NewPeer(conn, func(ctx context.Context) error { _, err := h.auth.CodeHostOwner(ctx, secret, true); return err })
	detach := h.codeHosts.Attach(owner, in.ID, peer)
	defer detach()
	ready, cancel := context.WithTimeout(ctx, 5*time.Second)
	err = wsjson.Write(ready, conn, codehosts.Response{Result: json.RawMessage(`{"ready":true}`)})
	cancel()
	if err != nil {
		return
	}
	readDone := make(chan struct{})
	go func() { defer close(readDone); _ = peer.Read(ctx); stop() }()
	defer func() { stop(); peer.Close(); <-readDone }()
	ticker := time.NewTicker(codehosts.HeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ping, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := conn.Ping(ping)
			if err == nil {
				_, err = h.auth.RenewCodeHost(ping, secret, in)
			}
			cancel()
			if err != nil {
				return
			}
		}
	}
}

func (h *Handler) codeHostRequest(w http.ResponseWriter, r *http.Request) {
	secret := token(r, h.sessionCookie)
	owner, err := h.auth.CodeHostOwner(r.Context(), secret, false)
	if err != nil {
		failure(w, err)
		return
	}
	var in codehosts.Request
	r.Body = http.MaxBytesReader(w, r.Body, codehosts.MaxRequestBytes)
	if !decode(w, r, &in) {
		return
	}
	if in.ID != "" {
		writeError(w, 400, "invalid_request", "Request IDs are assigned by Acta.", nil)
		return
	}
	switch in.Method {
	case codebases.ListMethod, codebases.AddMethod, codebases.DirectoryMethod, codethreads.StartMethod, codethreads.ListMethod, codethreads.ReadMethod, codethreads.SendMethod, codethreads.ReprocessMethod:
	default:
		writeError(w, 400, "unknown_method", "This host operation is not supported.", nil)
		return
	}
	peer := h.codeHosts.Get(owner, r.PathValue("id"))
	if peer == nil {
		writeError(w, 503, "host_disconnected", codehosts.ErrDisconnected.Error(), nil)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	response, err := peer.Call(ctx, in)
	if err != nil {
		code, message := "host_disconnected", codehosts.ErrDisconnected.Error()
		if errors.Is(err, context.DeadlineExceeded) {
			code = "host_timeout"
			message = "The host did not respond in time. Refresh to check whether the operation completed."
		}
		if errors.Is(err, codehosts.ErrBusy) {
			code = "host_busy"
			message = err.Error()
		}
		writeError(w, 503, code, message, nil)
		return
	}
	// Recheck the requesting browser too before returning any host data.
	if _, err = h.auth.CodeHostOwner(ctx, secret, false); err != nil {
		failure(w, err)
		return
	}
	if response.Error != nil {
		writeError(w, 422, "host_"+response.Error.Code, response.Error.Message, nil)
		return
	}
	if len(response.Result) == 0 || !json.Valid(response.Result) {
		writeError(w, 502, "invalid_host_response", "The host returned an invalid response.", nil)
		return
	}
	writeJSON(w, 200, response.Result)
}
