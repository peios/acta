package workspaces

import (
	"acta2/internal/accounts"
	"testing"
)

func TestScopedAccessAndInheritance(t *testing.T) {
	a := accounts.Account{ID: "human"}
	group := []GroupGrant{{ID: "g", Permissions: []string{Edit}}}
	if Resolve(a, false, nil, group).Allowed {
		t.Fatal("group admitted a non-member")
	}
	member := Resolve(a, true, []string{Members}, group)
	if !Subset([]string{Edit, Members}, member.Permissions) {
		t.Fatal("additive grants")
	}
	if !ResolveAgent(member, true, false, true, nil).Allowed {
		t.Fatal("all inheritance")
	}
	custom := ResolveAgent(member, true, false, false, []string{Edit, Permissions})
	if !Contains(custom.Permissions, Edit) || Contains(custom.Permissions, Permissions) {
		t.Fatal("custom ceiling")
	}
	if ResolveAgent(member, false, false, true, nil).Allowed {
		t.Fatal("selected access bypass")
	}
	if len(ResolveAgent(member, true, false, false, nil).Permissions) != 0 {
		t.Fatal("empty explicit set inherited")
	}
}
func TestWorkspaceProfile(t *testing.T) {
	for _, slug := range []string{"", "-bad", "bad-", "with space", "a/b", "é"} {
		if _, _, _, e := Profile("Name", slug, ""); e == nil {
			t.Fatalf("accepted %q", slug)
		}
	}
	name, slug, _, e := Profile(" Peios ", "Peios-Dev", "")
	if e != nil || name != "Peios" || slug != "peios-dev" {
		t.Fatal(name, slug, e)
	}
	if e := ValidateGrants(nil, []string{Permissions}, []string{Edit}); e == nil {
		t.Fatal("unowned grant")
	}
}
