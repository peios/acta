package integration

import (
	"acta/internal/accounts"
	"acta/internal/config"
	"acta/internal/httpapi"
	"net/http"
	"testing"
)

func TestBackupManagementPermissionBoundary(t *testing.T) {
	f := securityDatabase(t)
	a, member := permissionMember(t, f, "backup.member")
	cfg, err := config.Parse("http://localhost:8081", "")
	must(t, err)
	h := httpapi.New(f.service, f.security, accounts.NewProfileService(f.store), manager(f), cfg)
	c := client{t, h, map[string]*http.Cookie{}}
	c.request("GET", "backups", "", 401)
	c.cookies["acta_session"] = &http.Cookie{Name: "acta_session", Value: member.token}
	c.request("GET", "backups", "", 403)
	c.request("POST", "backups/jobs", `{"kind":"full"}`, 403)
	permissions := append(accounts.DefaultPermissions(), accounts.ManageBackups)
	grantPermissions(t, f, a.ID, permissions, false)
	c.request("GET", "backups", "", 200)
	c.request("POST", "backups/jobs", `{"kind":"full"}`, 503)
	grantPermissions(t, f, a.ID, permissions, true)
	c.request("GET", "backups", "", 403)
	// This permission cannot be inherited or explicitly delegated to an agent.
	owner := accounts.Account{DirectPermissions: permissions}
	for _, p := range accounts.AgentCatalogue(owner) {
		if p.ID == accounts.ManageBackups {
			t.Fatal("backup permission offered to agents")
		}
	}
}
