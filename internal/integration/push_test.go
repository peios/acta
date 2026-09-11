package integration

import (
	"acta/internal/auth"
	"acta/internal/config"
	"acta/internal/httpapi"
	"acta/internal/push"
	"acta/internal/threads"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"errors"
	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/google/uuid"
	"net/http"
	"testing"
	"time"
)

func TestPushQueueLifecycle(t *testing.T) {
	f := securityDatabase(t)
	ctx := t.Context()
	owner, err := f.service.Current(ctx, f.token)
	must(t, err)
	other, otherFixture := permissionMember(t, f, "other")
	receiver, err := ecdh.P256().GenerateKey(rand.Reader)
	must(t, err)
	sub := push.Subscription{Endpoint: "https://push.example/unique", Keys: webpush.Keys{Auth: base64.RawURLEncoding.EncodeToString(make([]byte, 16)), P256dh: base64.RawURLEncoding.EncodeToString(receiver.PublicKey().Bytes())}}
	subID, err := f.store.SavePushSubscription(ctx, owner.ID, auth.Digest(f.token), sub)
	must(t, err)
	again, err := f.store.SavePushSubscription(ctx, owner.ID, auth.Digest(f.token), sub)
	must(t, err)
	if again != subID {
		t.Fatal("subscription duplicated")
	}
	if _, err = f.store.SavePushSubscription(ctx, other.ID, auth.Digest(otherFixture.token), sub); !errors.Is(err, push.ErrSubscription) {
		t.Fatal("other owner stole subscription", err)
	}
	cfg, err := config.Parse("http://localhost:8081", "")
	must(t, err)
	c := client{t, httpapi.New(f.service, f.security, nil, nil, cfg, "public-key"), map[string]*http.Cookie{"acta_session": {Name: "acta_session", Value: f.token}}}
	c.request("POST", "push/subscribe", `{"endpoint":"http://localhost/","keys":{"auth":"invalid","p256dh":"invalid"}}`, 400)
	id := uuid.NewString()
	desc := threads.Descriptor{ID: id, RunID: id, Provider: "codex", CWD: "/tmp", Name: "Push review", State: "running", CreatedAt: time.Now().UTC(), Revision: 1}
	must(t, f.store.DiscoverThreads(ctx, owner.ID, []threads.Descriptor{desc}))
	var sequence int64
	appendTurn := func(turn, outcome string) []threads.Frame {
		t.Helper()
		sequence++
		p := threads.ProviderFrame{ThreadID: id, RunID: id, Provider: "codex", Sequence: sequence, ReceivedAt: desc.CreatedAt}
		debug := threads.NewFrame(p, "debug/resolved", map[string]any{"stream": "stdout", "raw": "{}", "reason": "mapped", "outputs": []threads.OutputReference{{OutputIndex: 1, Kind: "turn/completed"}}})
		frame := threads.NewFrame(p, "turn/completed", map[string]any{"turn_id": turn, "outcome": outcome, "error": nil, "started_at": nil, "completed_at": nil, "duration_ms": nil})
		frame.OutputIndex = 1
		frames := []threads.Frame{debug, frame}
		_, err := f.store.AppendThreadFrames(ctx, owner.ID, id, frames)
		must(t, err)
		return frames
	}
	due := func() {
		t.Helper()
		_, err := f.conn.Exec(ctx, `UPDATE push_deliveries SET due_at=now()-interval '1 second' WHERE NOT done`)
		must(t, err)
	}
	claim := func() *push.Delivery {
		t.Helper()
		d, err := f.store.ClaimPush(ctx)
		must(t, err)
		if d == nil {
			t.Fatal("missing delivery")
		}
		return d
	}
	frames := appendTurn("first", "completed")
	due()
	d := claim()
	if d.SubscriptionID != subID {
		t.Fatal(d)
	}
	if next, err := f.store.ClaimPush(ctx); err != nil || next != nil {
		t.Fatal("lease claimed twice", err)
	}
	n, err := f.store.PushNotice(ctx, owner.ID, auth.Digest(f.token), subID, d.NotificationID, d.Revision)
	must(t, err)
	if n.ThreadName != "Push review" {
		t.Fatal(n)
	}
	if _, err = f.store.PushNotice(ctx, other.ID, auth.Digest(otherFixture.token), subID, d.NotificationID, d.Revision); !errors.Is(err, auth.ErrNotFound) {
		t.Fatal("notification leaked", err)
	}
	// Failed send retries; a crashed/expired lease can be reclaimed. A stale
	// worker cannot acknowledge or delete the subscription after that reclaim.
	must(t, f.store.FinishPush(ctx, *d, 503))
	due()
	replacement := claim()
	if replacement.Lease == d.Lease {
		t.Fatal("lease reused")
	}
	must(t, f.store.FinishPush(ctx, *d, 410))
	// Simulate process loss without finishing: only the lease deadline expires.
	due()
	recovered := claim()
	if recovered.Lease == replacement.Lease {
		t.Fatal("crashed lease reused")
	}
	must(t, f.store.FinishPush(ctx, *replacement, 410))
	must(t, f.store.FinishPush(ctx, *recovered, 201))
	_, err = f.store.AppendThreadFrames(ctx, owner.ID, id, frames)
	must(t, err)
	due()
	if next, err := f.store.ClaimPush(ctx); err != nil || next != nil {
		t.Fatal("replay queued twice", err)
	}
	appendTurn("second", "completed")
	page, err := f.store.ThreadNotifications(ctx, owner.ID)
	must(t, err)
	var reads []threads.NotificationRead
	for _, n := range page.Items {
		reads = append(reads, threads.NotificationRead{ID: n.ID, Revision: n.Revision})
	}
	must(t, f.store.ReadThreadNotifications(ctx, owner.ID, reads))
	due()
	if next, err := f.store.ClaimPush(ctx); err != nil || next != nil {
		t.Fatal("read notice sent", err)
	}
	// The worker's authenticated fetch sees read and stale revisions as 404.
	c.request("GET", "push/notification?subscription="+subID+"&id="+d.NotificationID+"&revision=1", "", 404)
	appendTurn("third", "completed")
	due()
	gone := claim()
	must(t, f.store.FinishPush(ctx, *gone, 410))
	var count int
	must(t, f.conn.QueryRow(ctx, `SELECT count(*) FROM push_subscriptions`).Scan(&count))
	if count != 0 {
		t.Fatal("expired subscription kept")
	}
	subID, err = f.store.SavePushSubscription(ctx, owner.ID, auth.Digest(f.token), sub)
	must(t, err)
	appendTurn("logout", "completed")
	must(t, f.service.Logout(ctx, f.token))
	due()
	if next, err := f.store.ClaimPush(ctx); err != nil || next != nil {
		t.Fatal("push survived logout", err)
	}
	must(t, f.conn.QueryRow(ctx, `SELECT count(*) FROM push_subscriptions`).Scan(&count))
	if count != 0 {
		t.Fatal("subscription survived session deletion")
	}
}
