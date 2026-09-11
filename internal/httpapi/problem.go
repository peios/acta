package httpapi

import (
	"acta/internal/accounts"
	"acta/internal/auth"
	"acta/internal/documents"
	"acta/internal/guide"
	"acta/internal/memories"
	"acta/internal/migration"
	"acta/internal/tasks"
	"errors"
	"log/slog"
	"net/http"
)

type Problem struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
	Current *tasks.Conflict   `json:"current,omitempty"`
}

func classifyError(err error) (int, Problem) {
	var field *accounts.FieldError
	var conflict *tasks.Conflict
	switch {
	case errors.Is(err, migration.ErrConflict):
		return 409, Problem{Code: "migration_changed", Message: err.Error()}
	case errors.Is(err, auth.ErrSessionGrantsChanged):
		return 409, Problem{Code: "session_grants_changed", Message: err.Error()}
	case errors.Is(err, documents.ErrConflict):
		return 409, Problem{Code: "document_changed", Message: err.Error()}
	case errors.Is(err, guide.ErrConflict):
		return 409, Problem{Code: "guide_changed", Message: err.Error()}
	case errors.Is(err, memories.ErrConflict):
		return 409, Problem{Code: "memory_changed", Message: err.Error()}
	case errors.As(err, &conflict):
		return 409, Problem{Code: "conflict", Message: conflict.Error(), Current: conflict}
	case errors.Is(err, auth.ErrAccountDisabled):
		return 403, Problem{Code: "account_disabled", Message: "Account disabled. Contact your administrator.", Fields: nil}
	case errors.Is(err, auth.ErrMFARequired):
		return 403, Problem{Code: "mfa_required", Message: err.Error(), Fields: nil}
	case errors.Is(err, auth.ErrPermissionsChanged):
		return 409, Problem{Code: "permissions_changed", Message: err.Error(), Fields: nil}
	case errors.Is(err, auth.ErrForbidden):
		return 403, Problem{Code: "forbidden", Message: "You don’t have permission to do this.", Fields: nil}
	case errors.Is(err, auth.ErrAccountLink):
		return 410, Problem{Code: "account_link_expired", Message: "This invitation or recovery link is invalid or expired. Ask your administrator for a new link.", Fields: nil}
	case errors.Is(err, auth.ErrNotFound):
		return 404, Problem{Code: "not_found", Message: "The requested resource does not exist or is not accessible.", Fields: nil}
	case errors.As(err, &field):
		return 422, Problem{Code: "validation", Message: field.Message, Fields: map[string]string{field.Field: field.Message}}
	case errors.Is(err, auth.ErrDevice):
		return 410, Problem{Code: "device_expired", Message: err.Error(), Fields: nil}
	case errors.Is(err, auth.ErrFlow):
		return 409, Problem{Code: "flow_expired", Message: err.Error(), Fields: nil}
	case errors.Is(err, accounts.ErrProfileChanged):
		return 409, Problem{Code: "profile_changed", Message: "Your profile changed since this page was loaded. Load the latest profile before editing again.", Fields: nil}
	case errors.Is(err, auth.ErrSetupComplete):
		return 409, Problem{Code: "setup_complete", Message: "Acta is already set up. Sign in to continue.", Fields: nil}
	case errors.Is(err, auth.ErrSetupGrant):
		return 403, Problem{Code: "setup_expired", Message: "Setup authorisation expired. Enter your setup code again.", Fields: nil}
	case errors.Is(err, auth.ErrCredentials):
		return 401, Problem{Code: "invalid_credentials", Message: "Username or password is incorrect.", Fields: nil}
	case errors.Is(err, auth.ErrUnauthenticated), errors.Is(err, accounts.ErrAccountUnavailable):
		return 401, Problem{Code: "unauthenticated", Message: "Sign in to continue.", Fields: nil}
	case errors.Is(err, auth.ErrRateLimited):
		return 429, Problem{Code: "rate_limited", Message: "Too many attempts. Please wait five minutes before trying again.", Fields: nil}
	case errors.Is(err, auth.ErrBusy):
		return 503, Problem{Code: "busy", Message: "Acta is busy. Please try again in a moment.", Fields: nil}
	default:
		slog.Error("request failed", "error", err)
		return 503, Problem{Code: "unavailable", Message: "Acta couldn't complete this request. Please try again.", Fields: nil}
	}
}
func failure(w http.ResponseWriter, err error) {
	status, p := classifyError(err)
	if p.Code == "rate_limited" {
		w.Header().Set("Retry-After", "300")
	}
	if p.Code == "busy" {
		w.Header().Set("Retry-After", "2")
	}
	writeJSON(w, status, struct {
		Error Problem `json:"error"`
	}{p})
}
