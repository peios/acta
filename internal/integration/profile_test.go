package integration

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"acta/internal/accounts"
	"acta/internal/auth"
	"acta/internal/config"
	"acta/internal/httpapi"
	"acta/internal/postgres"
	"github.com/google/uuid"
)

func newAccount(t *testing.T, f fixture, name string, parent *string) accounts.Account {
	t.Helper()
	a := accounts.Account{ID: uuid.NewString(), Username: name, ParentID: parent, ProfileVersion: 1}
	_, err := f.conn.Exec(t.Context(), `INSERT INTO accounts(id,username,parent_id,direct_permissions) VALUES($1,$2,$3,$4)`, a.ID, name, parent, accounts.DefaultPermissions())
	must(t, err)
	return a
}

func TestProfileReservationsAndIdentity(t *testing.T) {
	f := database(t)
	ctx := t.Context()
	a := bootstrap(t, f, time.Now())
	child := newAccount(t, f, "reviewer", &a.ID)
	other := newAccount(t, f, "other", nil)
	service, _, err := auth.New(ctx, f.store)
	must(t, err)
	cfg, err := config.Parse("http://localhost:8081", "")
	must(t, err)
	security := securityForTest(t, f, service, cfg)
	login, err := security.PasswordLogin(ctx, "jack", "silver meadow orbit lamp", "test-binding", "profile-test", "Test browser")
	token := login.SessionToken
	must(t, err)
	p := accounts.NewProfileService(f.store)
	updated, err := p.Update(ctx, a.ID, 1, "JACK.SMITH", " Cafe\u0301 ")
	must(t, err)
	if updated.ID != a.ID || updated.Username != "jack.smith" || updated.DisplayName == nil || *updated.DisplayName != "Café" || updated.ProfileVersion != 2 {
		t.Fatalf("incorrect update: %+v", updated)
	}
	current, err := service.Current(ctx, token)
	must(t, err)
	if current.Username != updated.Username || current.ID != a.ID {
		t.Fatal("existing session lost identity or name")
	}
	if _, err = security.PasswordLogin(ctx, "jack", "silver meadow orbit lamp", "test-binding", "profile-test", "Test browser"); !errors.Is(err, auth.ErrCredentials) {
		t.Fatal("historical name still signs in", err)
	}
	_, err = security.PasswordLogin(ctx, "jack.smith", "silver meadow orbit lamp", "test-binding", "profile-test", "Test browser")
	must(t, err)
	childUpdated, err := auth.NewManagement(service, security, f.store).Agent(ctx, token, child.ID)
	must(t, err)
	if childUpdated.Handle() != "jack.smith/reviewer" || childUpdated.ProfileVersion != 2 || !slices.Contains(childUpdated.PreviousUsernames, "jack/reviewer") {
		t.Fatal("child handle did not follow parent")
	}
	for _, name := range []string{"jack", "jack.smith"} {
		_, err = f.store.UpdateProfile(ctx, other.ID, 1, name, nil)
		if !errors.Is(err, accounts.ErrNameUnavailable) {
			t.Fatalf("claimed another owner's name %s: %v", name, err)
		}
	}
	// A failed claim must not change either field, consume a version, or emit an event.
	var name string
	var display *string
	var version int64
	must(t, f.conn.QueryRow(ctx, `SELECT username,display_name,profile_version FROM accounts WHERE id=$1`, other.ID).Scan(&name, &display, &version))
	if name != "other" || display != nil || version != 1 {
		t.Fatal("failed rename partially persisted")
	}
	var events int
	must(t, f.conn.QueryRow(ctx, `SELECT count(*) FROM account_events WHERE account_id=$1`, other.ID).Scan(&events))
	if events != 0 {
		t.Fatal("failed rename emitted an event")
	}
	updated, err = p.Update(ctx, a.ID, 2, "JACK", "  ")
	must(t, err)
	if updated.Username != "jack" || updated.DisplayName != nil || updated.ProfileVersion != 3 {
		t.Fatal("owner could not reclaim or clear display name")
	}
	// Creation must obey exactly the same namespace as rename.
	_, err = f.conn.Exec(ctx, `INSERT INTO accounts(id,username) VALUES($1,'jack.smith')`, uuid.NewString())
	if err == nil {
		t.Fatal("creation stole a reserved name")
	}
	// Equal child segments under different parents remain independent.
	newAccount(t, f, "reviewer", &other.ID)
	_, err = f.conn.Exec(ctx, `UPDATE accounts SET disabled_at=now() WHERE id=$1`, a.ID)
	must(t, err)
	_, err = p.Update(ctx, a.ID, 3, "disabled-rename", "")
	if !errors.Is(err, accounts.ErrAccountUnavailable) {
		t.Fatal("disabled account updated", err)
	}
}

func TestConcurrentProfileClaimsAndStaleEdits(t *testing.T) {
	f := database(t)
	ctx := t.Context()
	second, err := postgres.Open(ctx, f.url)
	must(t, err)
	defer second.Close()
	stores := []*postgres.Store{f.store, second}
	a := newAccount(t, f, "one", nil)
	b := newAccount(t, f, "two", nil)
	var wins atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i, a := range []accounts.Account{a, b} {
		wg.Go(func() {
			<-start
			_, err := stores[i].UpdateProfile(ctx, a.ID, 1, "shared", nil)
			if err == nil {
				wins.Add(1)
			} else if !errors.Is(err, accounts.ErrNameUnavailable) {
				t.Error(err)
			}
		})
	}
	close(start)
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatal("name claim did not have exactly one winner")
	}
	c := newAccount(t, f, "third", nil)
	wins.Store(0)
	start = make(chan struct{})
	for i, name := range []string{"third-a", "third-b"} {
		wg.Go(func() {
			<-start
			_, err := stores[i].UpdateProfile(ctx, c.ID, 1, name, nil)
			if err == nil {
				wins.Add(1)
			} else if !errors.Is(err, accounts.ErrProfileChanged) {
				t.Error(err)
			}
		})
	}
	close(start)
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatal("stale edit overwrote profile")
	}
	var claims int
	must(t, f.conn.QueryRow(ctx, `SELECT count(*) FROM account_names WHERE account_id=$1`, c.ID).Scan(&claims))
	if claims != 2 {
		t.Fatalf("failed concurrent edit reserved a name: %d", claims)
	}
}

func TestPreviousUsernamesFollowClaims(t *testing.T) {
	f := database(t)
	ctx := t.Context()
	a := newAccount(t, f, "zebra", nil)
	other := newAccount(t, f, "other", nil)
	p := accounts.NewProfileService(f.store)
	_, err := p.Update(ctx, other.ID, 1, "someone-else", "")
	must(t, err)
	a, err = p.Update(ctx, a.ID, 1, "alpha", "")
	must(t, err)
	a, err = p.Update(ctx, a.ID, a.ProfileVersion, "middle", "")
	must(t, err)
	if !slices.Equal(a.PreviousUsernames, []string{"alpha", "zebra"}) {
		t.Fatalf("history must be sorted, owned, and exclude current: %v", a.PreviousUsernames)
	}
	a, err = p.Update(ctx, a.ID, a.ProfileVersion, "zebra", "")
	must(t, err)
	if !slices.Equal(a.PreviousUsernames, []string{"alpha", "middle"}) {
		t.Fatalf("reclaimed name stayed in history: %v", a.PreviousUsernames)
	}
}

func TestHTTPProfile(t *testing.T) {
	f := database(t)
	a := bootstrap(t, f, time.Now())
	other := newAccount(t, f, "taken", nil)
	service, _, err := auth.New(t.Context(), f.store)
	must(t, err)
	cfg, err := config.Parse("http://localhost:8081", "")
	must(t, err)
	c := client{t, httpapi.New(service, securityForTest(t, f, service, cfg), accounts.NewProfileService(f.store), nil, cfg), map[string]*http.Cookie{}}
	c.request("POST", "account/profile", `{"username":"new","profile_version":1}`, 401)
	c.request("POST", "login", `{"username":"jack","password":"silver meadow orbit lamp"}`, 200)
	var initial struct {
		PreviousUsernames []string `json:"previous_usernames"`
	}
	must(t, json.Unmarshal(c.request("GET", "account", "", 200).Body.Bytes(), &initial))
	if initial.PreviousUsernames == nil || len(initial.PreviousUsernames) != 0 {
		t.Fatal("new account must return an empty history array")
	}
	for _, payload := range []string{
		`{"username":"jack/agent","profile_version":1}`,
		`{"username":"jack","display_name":"two\nlines","profile_version":1}`,
		`{"username":"taken","profile_version":1}`,
	} {
		c.request("POST", "account/profile", payload, 422)
	}
	c.request("POST", "account/profile", `{"username":"new"}`, 409)
	c.request("POST", "account/profile", body(map[string]any{"username": "new", "profile_version": 1, "id": other.ID}), 400)
	c.request("POST", "account/profile", `{"username":"new","profile_version":1,"is_admin":false}`, 400)
	r := httptest.NewRequest("POST", "http://localhost:8081/api/account/profile", strings.NewReader(`{"username":"forged","profile_version":1}`))
	r.Header.Set("Origin", "https://evil.test")
	r.Header.Set("Content-Type", "application/json")
	r.AddCookie(c.cookies["acta_session"])
	w := httptest.NewRecorder()
	c.handler.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("cross-origin profile change accepted")
	}
	var result map[string]any
	must(t, json.Unmarshal(c.request("POST", "account/profile", `{"username":"NEW","display_name":" Jack Smith ","profile_version":1}`, 200).Body.Bytes(), &result))
	if result["id"] != a.ID || result["username"] != "new" || result["display_name"] != "Jack Smith" || result["profile_version"] != float64(2) || result["permissions"] == nil {
		t.Fatal(result)
	}
	c.request("POST", "account/profile", `{"username":"stale","profile_version":1}`, 409)
	must(t, json.Unmarshal(c.request("GET", "account", "", 200).Body.Bytes(), &result))
	if result["username"] != "new" || result["profile_version"] != float64(2) {
		t.Fatal("stale request changed profile")
	}
	previous, ok := result["previous_usernames"].([]any)
	if !ok || len(previous) != 1 || previous[0] != "jack" {
		t.Fatalf("incorrect private history response: %v", result)
	}
	c.request("POST", "account/profile", `{"username":"new","display_name":null,"profile_version":2}`, 200)
	c.request("POST", "logout", `{}`, 200)
	c.request("POST", "account/profile", `{"username":"revoked","profile_version":3}`, 401)
}
