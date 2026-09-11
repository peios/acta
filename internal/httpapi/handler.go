// Package httpapi translates HTTP requests into auth operations. It contains no SQL.
package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"acta/internal/accounts"
	"acta/internal/auth"
	"acta/internal/config"
	"acta/internal/hyperharness"
	"acta/internal/threads"
)

type Handler struct {
	pushKey                                   string
	harnesses                                 *hyperharness.Registry
	management                                *auth.Management
	auth                                      *auth.Service
	security                                  *auth.Security
	profiles                                  *accounts.ProfileService
	config                                    config.Config
	sessionCookie, setupCookie, bindingCookie string
}

func New(service *auth.Service, security *auth.Security, profiles *accounts.ProfileService, management *auth.Management, c config.Config, pushKey ...string) http.Handler {
	h := &Handler{harnesses: hyperharness.NewRegistry(), management: management, auth: service, security: security, bindingCookie: "acta_flow_browser", profiles: profiles, config: c, sessionCookie: "acta_session", setupCookie: "acta_setup"}
	if c.SecureCookies {
		h.sessionCookie = "__Host-acta_session"
		h.setupCookie = "__Host-acta_setup"
		h.bindingCookie = "__Host-acta_flow_browser"
	}
	if len(pushKey) > 0 {
		h.pushKey = pushKey[0]
	}
	mux := http.NewServeMux()
	h.backupRoutes(mux)
	h.updateRoutes(mux)
	h.pushRoutes(mux)
	h.harnessRoutes(mux)
	mux.HandleFunc("GET /api/notifications", h.threadNotifications)
	mux.HandleFunc("POST /api/notifications/read", h.threadNotificationsRead)
	h.threadRoutes(mux)
	h.workspaceRoutes(mux)
	h.taskRoutes(mux)
	h.memoryRoutes(mux)
	h.guideRoutes(mux)
	mux.HandleFunc("GET /api/agents", h.agents)
	mux.HandleFunc("POST /api/agents", h.createAgent)
	mux.HandleFunc("GET /api/agents/permissions", h.agentCatalogue)
	mux.HandleFunc("GET /api/agents/{id}", h.agent)
	mux.HandleFunc("POST /api/agents/{id}/profile", h.agentProfile)
	mux.HandleFunc("POST /api/agents/{id}/permissions", h.agentPermissions)
	mux.HandleFunc("POST /api/agents/{id}/disabled", h.agentDisabled)
	mux.HandleFunc("GET /api/agents/{id}/sessions", h.agentSessions)
	mux.HandleFunc("POST /api/agents/{id}/revoke", h.agentRevoke)
	mux.HandleFunc("GET /.well-known/oauth-authorization-server", h.oauthMetadata)
	mux.HandleFunc("GET /.well-known/oauth-protected-resource", h.protectedResource)
	mux.HandleFunc("GET /.well-known/oauth-protected-resource/mcp", h.protectedResource)
	mux.HandleFunc("POST /oauth/register", h.oauthRegister)
	mux.HandleFunc("GET /oauth/authorize", h.oauthAuthorize)
	mux.HandleFunc("POST /oauth/token", h.oauthToken)
	mux.HandleFunc("POST /oauth/revoke", h.oauthRevoke)
	mux.HandleFunc("GET /api/oauth/consent", h.oauthConsent)
	mux.HandleFunc("POST /api/oauth/approve", h.oauthApprove)
	mux.HandleFunc("/mcp", h.mcp)
	mux.HandleFunc("POST /api/device/start", h.deviceStart)
	mux.HandleFunc("POST /api/device/poll", h.devicePoll)
	mux.HandleFunc("GET /api/device", h.deviceInfo)
	mux.HandleFunc("POST /api/device/approve", h.deviceApprove)
	mux.HandleFunc("GET /api/groups", h.groups)
	mux.HandleFunc("POST /api/groups", h.createGroup)
	mux.HandleFunc("GET /api/groups/{id}", h.group)
	mux.HandleFunc("POST /api/groups/{id}/profile", h.groupProfile)
	mux.HandleFunc("POST /api/groups/{id}/permissions", h.groupPermissions)
	mux.HandleFunc("GET /api/groups/{id}/members", h.groupMembers)
	mux.HandleFunc("POST /api/groups/{id}/members/{account}", h.groupMembership)
	mux.HandleFunc("GET /api/users", h.users)
	mux.HandleFunc("POST /api/users", h.createUser)
	mux.HandleFunc("GET /api/users/{id}", h.user)
	mux.HandleFunc("POST /api/users/{id}/profile", h.userProfile)
	mux.HandleFunc("GET /api/permissions", h.permissionCatalogue)
	mux.HandleFunc("POST /api/users/{id}/permissions", h.userPermissions)
	mux.HandleFunc("POST /api/users/{id}/link", h.userLink)
	mux.HandleFunc("POST /api/users/{id}/disabled", h.userDisabled)
	mux.HandleFunc("POST /api/account-link/open", h.openAccountLink)
	mux.HandleFunc("POST /api/account-link/redeem", h.redeemAccountLink)
	mux.HandleFunc("GET /api/setup", h.state)
	mux.HandleFunc("POST /api/setup/unlock", h.unlock)
	mux.HandleFunc("POST /api/setup/complete", h.complete)
	mux.HandleFunc("POST /api/login", h.login)
	mux.HandleFunc("GET /api/account", h.account)
	mux.HandleFunc("POST /api/account/profile", h.updateProfile)
	mux.HandleFunc("POST /api/logout", h.logout)
	mux.HandleFunc("GET /api/security", h.securityView)
	mux.HandleFunc("POST /api/security/flow", h.securityBegin)
	mux.HandleFunc("POST /api/security/advance", h.securityAdvance)
	mux.HandleFunc("POST /api/security/cancel", h.securityCancel)
	mux.HandleFunc("POST /api/security/revoke", h.securityRevoke)
	mux.HandleFunc("POST /api/security/sessions/{id}/grants", h.securitySessionGrants)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, 404, "not_found", "This endpoint does not exist.", nil)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c.RecoveryMode && (r.URL.Path == "/mcp" || strings.HasPrefix(r.URL.Path, "/oauth/") || strings.HasPrefix(r.URL.Path, "/.well-known/") || strings.HasPrefix(r.URL.Path, "/api/harnesses") || strings.HasPrefix(r.URL.Path, "/api/backups") || (r.Method != "GET" && r.Method != "HEAD" && (strings.HasPrefix(r.URL.Path, "/api/threads") || strings.HasPrefix(r.URL.Path, "/api/push")))) {
			writeError(w, 503, "recovery_mode", "External integrations are disabled in this recovery verification instance.", nil)
			return
		}
		ctx, cancel := context.WithCancel(r.Context())
		if !harnessStream(r.URL.Path) {
			cancel()
			timeout := 15 * time.Second
			if documentUpload(r) {
				timeout = 2 * time.Minute
				// Override server defaults for bounded file transfers only.
				control := http.NewResponseController(w)
				_ = control.SetReadDeadline(time.Now().Add(timeout))
				_ = control.SetWriteDeadline(time.Now().Add(timeout))
			}
			ctx, cancel = context.WithTimeout(r.Context(), timeout)
		}
		defer cancel()
		r = r.WithContext(ctx)
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if !c.Hosts[r.Host] {
			writeError(w, 400, "invalid_host", "This address is not configured for Acta.", nil)
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			// Browser mutations require an exact Origin. Native protocol endpoints
			// authenticate independently and never accept browser-cookie authority.
			native := r.Header.Get("Origin") == "" && (strings.HasPrefix(r.Header.Get("Authorization"), "Bearer cli_") || r.URL.Path == "/api/device/start" || r.URL.Path == "/api/device/poll" || r.URL.Path == "/oauth/register" || r.URL.Path == "/oauth/token" || r.URL.Path == "/oauth/revoke" || r.URL.Path == "/mcp")
			if !native && !c.Origins[r.Header.Get("Origin")] {
				writeError(w, 403, "invalid_origin", "This request did not come from this Acta installation.", nil)
				return
			}
			contentType := "application/json"
			if r.URL.Path == "/oauth/token" || r.URL.Path == "/oauth/revoke" {
				contentType = "application/x-www-form-urlencoded"
			}
			if documentUpload(r) {
				contentType = "multipart/form-data"
			}
			if r.Method == "POST" && strings.Split(r.Header.Get("Content-Type"), ";")[0] != contentType {
				writeError(w, 415, "invalid_content_type", "Send "+contentType+".", nil)
				return
			}
		}
		mux.ServeHTTP(w, r)
	})
}
func token(r *http.Request, name string) string {
	if strings.HasSuffix(name, "acta_session") && r.Header.Get("Authorization") != "" {
		value := r.Header.Get("Authorization")
		if strings.HasPrefix(value, "Bearer cli_") {
			return strings.TrimPrefix(value, "Bearer ")
		}
		return ""
	}
	c, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	return c.Value
}
func (h *Handler) cookie(w http.ResponseWriter, name, value string, lifetime time.Duration) {
	maxAge := int(lifetime.Seconds())
	expires := time.Now().Add(lifetime)
	if value == "" {
		maxAge = -1
		expires = time.Unix(1, 0)
	}
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", HttpOnly: true, Secure: h.config.SecureCookies, SameSite: http.SameSiteStrictMode, MaxAge: maxAge, Expires: expires})
}
func decode(w http.ResponseWriter, r *http.Request, value any) bool {
	limit := int64(64 * 1024)
	if strings.HasPrefix(r.URL.Path, "/api/tasks/") || strings.HasSuffix(r.URL.Path, "/tasks") {
		limit = 256 * 1024
	}
	if strings.HasPrefix(r.URL.Path, "/api/threads/") && strings.HasSuffix(r.URL.Path, "/control") {
		limit = threads.MaxMessageWire
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil {
		writeError(w, 400, "invalid_request", "Check the form and try again.", nil)
		return false
	}
	if err := d.Decode(new(any)); err != io.EOF {
		writeError(w, 400, "invalid_request", "Send one JSON object.", nil)
		return false
	}
	return true
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, status int, code, message string, fields map[string]string) {
	writeJSON(w, status, map[string]any{"error": map[string]any{"code": code, "message": message, "fields": fields}})
}

func (h *Handler) state(w http.ResponseWriter, r *http.Request) {
	complete, unlocked, err := h.auth.State(r.Context(), token(r, h.setupCookie))
	if err != nil {
		failure(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"complete": complete, "unlocked": unlocked, "password_min": auth.PasswordMin, "password_max": auth.PasswordMax})
}
func (h *Handler) unlock(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Code string `json:"code"`
	}
	if !decode(w, r, &input) {
		return
	}
	grant, err := h.auth.Unlock(r.Context(), strings.TrimSpace(input.Code), h.address(r))
	if err != nil {
		failure(w, err)
		return
	}
	h.cookie(w, h.setupCookie, grant, auth.SetupLifetime)
	writeJSON(w, 200, map[string]bool{"unlocked": true})
}
func (h *Handler) complete(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Username    string `json:"username"`
		Password    string `json:"password"`
		DisplayName string `json:"display_name"`
	}
	if !decode(w, r, &input) {
		return
	}
	_, err := h.auth.Complete(r.Context(), token(r, h.setupCookie), input.Username, input.Password, input.DisplayName)
	if err != nil {
		failure(w, err)
		return
	}
	h.cookie(w, h.setupCookie, "", 0)
	writeJSON(w, 201, map[string]bool{"complete": true})
}
func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decode(w, r, &input) {
		return
	}
	binding, err := h.binding(w, r)
	if err != nil {
		failure(w, err)
		return
	}
	result, err := h.security.PasswordLogin(r.Context(), input.Username, input.Password, binding, h.address(r), sessionDescription(r.UserAgent()))
	if err != nil {
		failure(w, err)
		return
	}
	h.flowResponse(w, r, result)
}

func (h *Handler) account(w http.ResponseWriter, r *http.Request) {
	a, err := h.auth.Authenticated(r.Context(), token(r, h.sessionCookie))
	if err != nil {
		failure(w, err)
		return
	}
	writeAccount(w, a)
}
func writeAccount(w http.ResponseWriter, a accounts.Account) {
	writeJSON(w, 200, accountData(a))
}
func (h *Handler) updateProfile(w http.ResponseWriter, r *http.Request) {
	a, err := h.auth.Current(r.Context(), token(r, h.sessionCookie))
	if err != nil {
		failure(w, err)
		return
	}
	var input struct {
		Username    string  `json:"username"`
		DisplayName *string `json:"display_name"`
		Version     int64   `json:"profile_version"`
	}
	if !decode(w, r, &input) {
		return
	}
	name := ""
	if input.DisplayName != nil {
		name = *input.DisplayName
	}
	a, err = h.profiles.Update(r.Context(), a.ID, input.Version, input.Username, name)
	if err != nil {
		failure(w, err)
		return
	}
	writeAccount(w, a)
}
func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	if err := h.auth.Logout(r.Context(), token(r, h.sessionCookie)); err != nil {
		failure(w, err)
		return
	}
	h.cookie(w, h.sessionCookie, "", 0)
	writeJSON(w, 200, map[string]bool{"authenticated": false})
}

func accountData(a accounts.Account) map[string]any {
	return map[string]any{"id": a.ID, "username": a.Handle(), "username_segment": a.Username, "owner_id": a.ParentID, "owner_username": a.ParentUsername, "display_name": a.DisplayName, "last_active_superuser": a.LastActiveSuperuser, "permissions": accounts.ResolvePermissions(a), "direct_permissions": a.DirectPermissions, "require_mfa": accounts.RequiresMFA(a), "direct_require_mfa": a.RequireMFA, "groups": a.Groups, "can_create_users": accounts.CanCreateUser(a), "mfa_setup_required": accounts.RequiresMFASetup(a), "permissions_version": a.PermissionsVersion, "profile_version": a.ProfileVersion, "previous_usernames": a.PreviousUsernames, "status": a.Status(), "pending": a.Pending, "created_at": a.CreatedAt}
}
