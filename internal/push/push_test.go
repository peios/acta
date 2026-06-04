package push

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/peios/acta/internal/store"
	"github.com/peios/acta/internal/store/memstore"
)

// A throwaway VAPID pair (from `go run ./cmd/acta-vapid`) so the real send path,
// including the signed VAPID header, runs without reaching a push service.
const (
	testPub  = "BFbdFE85plfk-WNX-NuwNFS65a83oB898guAMIfAtbajVFHhUWMdZ5HZtM-Rk63zVELCpwBIcdkXNQU_3nJ-imI"
	testPriv = "Z8MS_QrbTkj8J93i8LWxx1v--sBoPjbLUCws54x_Vdg"
)

// stubClient captures the outbound request and returns a canned status instead
// of hitting the network.
type stubClient struct {
	status int
	got    chan *http.Request
}

func (c *stubClient) Do(req *http.Request) (*http.Response, error) {
	if req.Body != nil {
		_, _ = io.Copy(io.Discard, req.Body)
		_ = req.Body.Close()
	}
	select {
	case c.got <- req:
	default:
	}
	return &http.Response{
		StatusCode: c.status,
		Body:       io.NopCloser(strings.NewReader("")),
		Header:     make(http.Header),
	}, nil
}

// validSub builds a subscription with a real P-256 public point and a 16-byte
// auth secret, so webpush-go's payload encryption succeeds (it ECDHs against the
// point; it doesn't require us to be able to decrypt).
func validSub(t *testing.T, endpoint, user string) store.PushSubscription {
	t.Helper()
	priv, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	auth := make([]byte, 16)
	if _, err := rand.Read(auth); err != nil {
		t.Fatal(err)
	}
	b64 := base64.RawURLEncoding
	return store.PushSubscription{
		Endpoint: endpoint,
		UserID:   user,
		P256dh:   b64.EncodeToString(priv.PublicKey().Bytes()),
		Auth:     b64.EncodeToString(auth),
	}
}

func mention() store.Notification {
	return store.Notification{
		Kind: store.NotificationMention, ActorName: "Bo",
		WorkspaceSlug: "gen", ItemID: "i1", ItemTitle: "Login bug", Excerpt: "hey there",
	}
}

func TestNotifyUserSendsToSubscription(t *testing.T) {
	ms := memstore.New()
	stub := &stubClient{status: 201, got: make(chan *http.Request, 1)}
	s := New(ms, Config{PublicKey: testPub, PrivateKey: testPriv, Subject: "mailto:t@acta.test"},
		WithHTTPClient(stub))
	defer s.Close()

	ctx := context.Background()
	if err := ms.CreatePushSubscription(ctx, validSub(t, "https://push.example/aaa", "u1")); err != nil {
		t.Fatal(err)
	}
	s.NotifyUser(ctx, "u1", mention())

	select {
	case req := <-stub.got:
		if got := req.URL.String(); got != "https://push.example/aaa" {
			t.Errorf("posted to %q, want the subscription endpoint", got)
		}
		if req.Header.Get("Authorization") == "" {
			t.Error("missing VAPID Authorization header")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no push delivered")
	}
}

func TestNotifyUserNoSubscriptionsIsQuiet(t *testing.T) {
	ms := memstore.New()
	stub := &stubClient{status: 201, got: make(chan *http.Request, 1)}
	s := New(ms, Config{PublicKey: testPub, PrivateKey: testPriv}, WithHTTPClient(stub))
	defer s.Close()

	s.NotifyUser(context.Background(), "nobody", mention())
	select {
	case <-stub.got:
		t.Fatal("sent a push for a user with no subscriptions")
	case <-time.After(150 * time.Millisecond):
	}
}

func TestPruneOnGone(t *testing.T) {
	ms := memstore.New()
	stub := &stubClient{status: http.StatusGone, got: make(chan *http.Request, 1)}
	s := New(ms, Config{PublicKey: testPub, PrivateKey: testPriv, Subject: "mailto:t@acta.test"},
		WithHTTPClient(stub))
	defer s.Close()

	ctx := context.Background()
	if err := ms.CreatePushSubscription(ctx, validSub(t, "https://push.example/gone", "u1")); err != nil {
		t.Fatal(err)
	}
	s.NotifyUser(ctx, "u1", mention())

	<-stub.got // the send happened; the prune follows once it reads the 410
	deadline := time.Now().Add(2 * time.Second)
	for {
		subs, err := ms.PushSubscriptionsByUser(ctx, "u1")
		if err != nil {
			t.Fatal(err)
		}
		if len(subs) == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("subscription not pruned after a 410 Gone")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestUnsubscribeScopedToOwner(t *testing.T) {
	ms := memstore.New()
	s := New(ms, Config{PublicKey: testPub, PrivateKey: testPriv})
	defer s.Close()
	ctx := context.Background()

	sub := store.PushSubscription{Endpoint: "e1", UserID: "owner", P256dh: "p", Auth: "a"}
	if err := ms.CreatePushSubscription(ctx, sub); err != nil {
		t.Fatal(err)
	}

	// A different user can't drop a subscription they don't own.
	if err := s.Unsubscribe(ctx, "intruder", "e1"); err != nil {
		t.Fatal(err)
	}
	if subs, _ := ms.PushSubscriptionsByUser(ctx, "owner"); len(subs) != 1 {
		t.Fatal("intruder removed another user's subscription")
	}
	// The owner can.
	if err := s.Unsubscribe(ctx, "owner", "e1"); err != nil {
		t.Fatal(err)
	}
	if subs, _ := ms.PushSubscriptionsByUser(ctx, "owner"); len(subs) != 0 {
		t.Fatal("owner's unsubscribe didn't remove the subscription")
	}
}

func TestToMessage(t *testing.T) {
	m := toMessage(mention())
	if m.Title != "Bo mentioned you" {
		t.Errorf("title = %q", m.Title)
	}
	if m.URL != "/gen?item=i1" {
		t.Errorf("url = %q, want the item deep link", m.URL)
	}
	if m.Tag != "i1" {
		t.Errorf("tag = %q, want the item id", m.Tag)
	}
	if !strings.Contains(m.Body, "Login bug") || !strings.Contains(m.Body, "hey there") {
		t.Errorf("body = %q, want item title + excerpt", m.Body)
	}
}
