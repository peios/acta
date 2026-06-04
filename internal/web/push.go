package web

import (
	"encoding/json"
	"net/http"

	"github.com/peios/acta/internal/store"
)

// serviceWorker serves the Web Push service worker. It must be served from the
// origin root so its control scope is "/" (a worker only controls pages at or
// below its own path), so it has its own route rather than living under
// /static/. no-cache lets an updated worker propagate on the next visit.
func (h *handlers) serviceWorker(w http.ResponseWriter, _ *http.Request) {
	b, err := staticFS.ReadFile("static/sw.js")
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Service-Worker-Allowed", "/")
	_, _ = w.Write(b)
}

// pushSubscribe records the posted browser PushSubscription for the signed-in
// user. The body is the browser's PushSubscription.toJSON() shape verbatim.
func (h *handlers) pushSubscribe(w http.ResponseWriter, r *http.Request) {
	if h.push == nil {
		http.Error(w, "push notifications are not configured", http.StatusServiceUnavailable)
		return
	}
	p := principalFrom(r.Context())
	if p == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req struct {
		Endpoint string `json:"endpoint"`
		Keys     struct {
			P256dh string `json:"p256dh"`
			Auth   string `json:"auth"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil ||
		req.Endpoint == "" || req.Keys.P256dh == "" || req.Keys.Auth == "" {
		http.Error(w, "malformed subscription", http.StatusBadRequest)
		return
	}
	if err := h.push.Subscribe(r.Context(), p.ID, store.PushSubscription{
		Endpoint: req.Endpoint,
		P256dh:   req.Keys.P256dh,
		Auth:     req.Keys.Auth,
	}); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// pushUnsubscribe drops the posted endpoint's subscription (the user turned
// notifications off on this device). Scoped to the signed-in user by the sender.
func (h *handlers) pushUnsubscribe(w http.ResponseWriter, r *http.Request) {
	if h.push == nil {
		http.Error(w, "push notifications are not configured", http.StatusServiceUnavailable)
		return
	}
	p := principalFrom(r.Context())
	if p == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req struct {
		Endpoint string `json:"endpoint"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Endpoint == "" {
		http.Error(w, "malformed request", http.StatusBadRequest)
		return
	}
	if err := h.push.Unsubscribe(r.Context(), p.ID, req.Endpoint); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
