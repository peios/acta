// Package push is Acta's Web Push delivery channel: a second, best-effort way a
// notification reaches a user, parallel to the in-app inbox and the live SSE
// stream. It owns browser push subscriptions and fans notifications out to them
// over the IETF Web Push protocol (RFC 8030/8291/8292) via webpush-go.
//
// Delivery is asynchronous and best-effort, mirroring the live hub: a bounded
// worker pool drains a queue so a mutation request never blocks on a slow push
// service, and an over-full queue drops rather than stalls — the inbox is the
// durable record, push is only the nudge. A subscription the push service
// reports gone (404/410) is pruned.
package push

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"

	"github.com/peios/acta/internal/store"
)

// Store is the slice of persistence the push subsystem needs.
type Store interface {
	CreatePushSubscription(ctx context.Context, sub store.PushSubscription) error
	PushSubscriptionsByUser(ctx context.Context, userID string) ([]store.PushSubscription, error)
	DeletePushSubscription(ctx context.Context, endpoint string) error
}

// Config is the VAPID identity. PublicKey reaches the browser to authenticate a
// subscription; PrivateKey signs the JWT sent to the push service; Subject is
// the contact the push service can reach about our traffic.
type Config struct {
	PublicKey  string
	PrivateKey string
	Subject    string
}

const (
	workerCount = 4
	queueDepth  = 256
	sendTimeout = 10 * time.Second
	pushTTL     = 24 * 60 * 60 // seconds a push service holds an undelivered message
)

// Sender manages subscriptions and delivers notifications to them. The zero
// value is not usable; call New. It is safe for concurrent use.
type Sender struct {
	store     Store
	cfg       Config
	client    *webpush.HTTPClient
	jobs      chan job
	quit      chan struct{}
	closeOnce sync.Once
}

type job struct {
	userID  string
	payload []byte
}

// Option configures a Sender.
type Option func(*Sender)

// WithHTTPClient overrides the client used to POST to push services — used by
// tests to intercept the request without reaching the network.
func WithHTTPClient(c webpush.HTTPClient) Option {
	return func(s *Sender) { s.client = &c }
}

// New starts a Sender and its worker pool. The workers run for the process
// lifetime (Acta is single-instance), matching the live hub's simplicity.
func New(st Store, cfg Config, opts ...Option) *Sender {
	s := &Sender{
		store: st,
		cfg:   cfg,
		jobs:  make(chan job, queueDepth),
		quit:  make(chan struct{}),
	}
	for _, o := range opts {
		o(s)
	}
	for range workerCount {
		go s.worker()
	}
	return s
}

// Close stops the worker pool. In-flight sends finish or time out; queued-but-
// unstarted jobs are dropped. Safe to call more than once.
func (s *Sender) Close() { s.closeOnce.Do(func() { close(s.quit) }) }

// PublicKey is the VAPID public key a browser needs to subscribe.
func (s *Sender) PublicKey() string { return s.cfg.PublicKey }

// Subscribe persists a browser's push registration for userID (upsert by
// endpoint, so re-subscribing the same browser refreshes rather than dupes).
func (s *Sender) Subscribe(ctx context.Context, userID string, sub store.PushSubscription) error {
	sub.UserID = userID
	return s.store.CreatePushSubscription(ctx, sub)
}

// Unsubscribe removes a registration the user owns (turning notifications off
// on that device). It is scoped to userID so a request can't drop another
// person's device by presenting their endpoint; an unowned or already-gone
// endpoint is a no-op.
func (s *Sender) Unsubscribe(ctx context.Context, userID, endpoint string) error {
	subs, err := s.store.PushSubscriptionsByUser(ctx, userID)
	if err != nil {
		return err
	}
	for _, sub := range subs {
		if sub.Endpoint == endpoint {
			return s.store.DeletePushSubscription(ctx, endpoint)
		}
	}
	return nil
}

// NotifyUser queues a Web Push of n to every device userID has subscribed and
// returns immediately; delivery happens on the worker pool. A full queue drops
// the nudge (logged) rather than blocking the caller. ctx is not used for
// delivery — that outlives the request — so only its cancellation of the
// enqueue is honoured implicitly by the non-blocking send.
func (s *Sender) NotifyUser(_ context.Context, userID string, n store.Notification) {
	payload, err := json.Marshal(toMessage(n))
	if err != nil {
		slog.Error("push: marshal payload", "err", err)
		return
	}
	select {
	case s.jobs <- job{userID: userID, payload: payload}:
	case <-s.quit:
	default:
		slog.Warn("push: queue full, dropping notification", "user", userID)
	}
}

func (s *Sender) worker() {
	for {
		select {
		case j := <-s.jobs:
			s.deliver(j)
		case <-s.quit:
			return
		}
	}
}

func (s *Sender) deliver(j job) {
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	subs, err := s.store.PushSubscriptionsByUser(ctx, j.userID)
	if err != nil {
		slog.Error("push: load subscriptions", "user", j.userID, "err", err)
		return
	}
	for _, sub := range subs {
		s.send(ctx, sub, j.payload)
	}
}

func (s *Sender) send(ctx context.Context, sub store.PushSubscription, payload []byte) {
	opts := &webpush.Options{
		Subscriber:      s.cfg.Subject,
		VAPIDPublicKey:  s.cfg.PublicKey,
		VAPIDPrivateKey: s.cfg.PrivateKey,
		TTL:             pushTTL,
		Urgency:         webpush.UrgencyNormal,
	}
	if s.client != nil {
		opts.HTTPClient = *s.client
	}
	resp, err := webpush.SendNotificationWithContext(ctx, payload, &webpush.Subscription{
		Endpoint: sub.Endpoint,
		Keys:     webpush.Keys{P256dh: sub.P256dh, Auth: sub.Auth},
	}, opts)
	if err != nil {
		slog.Warn("push: send failed", "endpoint", endpointHost(sub.Endpoint), "err", err)
		return
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		// The browser unsubscribed or the push service expired it: prune so we
		// stop trying. Use a fresh context — ctx may be near its send deadline.
		dctx, c := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer c()
		if err := s.store.DeletePushSubscription(dctx, sub.Endpoint); err != nil {
			slog.Error("push: prune dead subscription", "err", err)
		}
	case resp.StatusCode >= 300:
		slog.Warn("push: unexpected status", "status", resp.StatusCode, "endpoint", endpointHost(sub.Endpoint))
	}
}

// message is the JSON the service worker reads to build the notification.
type message struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	URL   string `json:"url"`
	Tag   string `json:"tag"`
}

func toMessage(n store.Notification) message {
	if n.Kind == store.NotificationSession {
		// The session is the subject: its title heads the push, the phrase
		// ("needs permission for Bash: git push") is the body, and repeated
		// pings about one session collapse into the latest.
		title := n.ItemTitle
		if title == "" {
			title = "Claude session"
		}
		return message{Title: title, Body: n.ActorName + " " + n.Summary, URL: "/account/sessions/" + n.ItemID, Tag: "session-" + n.ItemID}
	}
	title := n.ActorName
	switch n.Kind {
	case store.NotificationMention:
		title = n.ActorName + " mentioned you"
	}
	if title == "" {
		title = "Acta"
	}
	body := n.Excerpt
	if n.ItemTitle != "" && body != "" {
		body = n.ItemTitle + " — " + body
	} else if n.ItemTitle != "" {
		body = n.ItemTitle
	}
	return message{
		Title: title,
		Body:  body,
		// The item's canonical deep link: the board with ?item= opens its modal.
		URL: "/" + n.WorkspaceSlug + "?item=" + n.ItemID,
		Tag: n.ItemID, // collapse repeated pings about the same item
	}
}

// endpointHost is the host of a push endpoint, for logs — the full endpoint is
// a capability URL with a secret token, so we never log it whole.
func endpointHost(endpoint string) string {
	if u, err := url.Parse(endpoint); err == nil && u.Host != "" {
		return u.Host
	}
	return "?"
}
