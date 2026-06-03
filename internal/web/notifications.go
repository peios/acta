package web

import (
	"net/http"

	"github.com/peios/acta/internal/httpx"
)

// notificationOpen marks one notification read and redirects to its target
// item. It's a GET (the bell rows are plain links, so the dropdown needs no
// JS), and the redirect target arrives as ?to= — validated as a same-origin
// path before use, so it can't be turned into an open redirect. Marking is
// scoped to the signed-in principal and idempotent.
func (h *handlers) notificationOpen(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	if p == nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	_ = h.board.MarkNotificationRead(r.Context(), r.PathValue("id"), p.ID)
	http.Redirect(w, r, httpx.SafeReturnTo(r.URL.Query().Get("to")), http.StatusSeeOther)
}

// notificationsReadAll clears the signed-in principal's whole unread set and
// returns to the page the form was submitted from (carried in return_to, which
// SafeReturnTo confines to a same-origin path).
func (h *handlers) notificationsReadAll(w http.ResponseWriter, r *http.Request) {
	if p := principalFrom(r.Context()); p != nil {
		_ = h.board.MarkAllNotificationsRead(r.Context(), p.ID)
	}
	http.Redirect(w, r, httpx.SafeReturnTo(r.FormValue("return_to")), http.StatusSeeOther)
}
