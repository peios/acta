package web

import (
	"context"
	"net/http"
	"time"
)

// HealthHandler serves the liveness and readiness probes. It is mounted ahead
// of the application handler (outside auth, CSRF, and the access log) so an
// uptime monitor or load balancer can poll it cheaply and without noise.
//
//   - GET /healthz — liveness: the process is up. Always 200.
//   - GET /readyz  — readiness: dependencies are reachable. Calls ready (a DB
//     ping); 200 when it succeeds, 503 when it doesn't. A nil ready is treated
//     as always-ready.
func HealthHandler(ready func(context.Context) error) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeOK(w)
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if ready != nil {
			ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			defer cancel()
			if err := ready(ctx); err != nil {
				http.Error(w, "not ready", http.StatusServiceUnavailable)
				return
			}
		}
		writeOK(w)
	})
	return mux
}

func writeOK(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok\n"))
}
