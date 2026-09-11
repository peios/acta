package integration

import (
	"acta/internal/accounts"
	"acta/internal/config"
	"acta/internal/httpapi"
	"net/http"
	"testing"
)

func TestUpdateManagementRequiresHumanSuperuser(t *testing.T) {
	f := securityDatabase(t)
	a, member := permissionMember(t, f, "update.member")
	cfg, err := config.Parse("http://localhost:8081", "")
	must(t, err)
	h := httpapi.New(f.service, f.security, accounts.NewProfileService(f.store), manager(f), cfg)
	c := client{t, h, map[string]*http.Cookie{}}
	c.request("GET", "updates", "", 401)
	c.cookies["acta_session"] = &http.Cookie{Name: "acta_session", Value: member.token}
	for _, p := range []string{"updates/check", "updates/install", "updates/retry"} {
		c.request("POST", p, `{}`, 403)
	}
	grantPermissions(t, f, a.ID, append(accounts.DefaultPermissions(), accounts.ManageBackups), false)
	c.request("GET", "updates", "", 403)
	grantPermissions(t, f, a.ID, append(accounts.DefaultPermissions(), accounts.Superuser), false)
	c.request("GET", "updates", "", 200)
	c.request("POST", "updates/install", `{"id":"x"}`, 503)
	grantPermissions(t, f, a.ID, append(accounts.DefaultPermissions(), accounts.Superuser), true)
	c.request("GET", "updates", "", 403)
}
