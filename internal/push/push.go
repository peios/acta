// Package push owns browser push subscriptions and reliable, provider-neutral
// delivery. The database queue is authoritative; push receipts are best effort.
package push

import (
	"context"
	"crypto/ecdh"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"acta2/internal/threads"
	webpush "github.com/SherClockHolmes/webpush-go"
)

type Subscription = webpush.Subscription

var ErrSubscription = errors.New("invalid or unavailable push subscription")

type Store interface {
	SavePushSubscription(context.Context, string, []byte, Subscription) (string, error)
	DeletePushSubscription(context.Context, string, []byte, string) error
	PushNotice(context.Context, string, []byte, string, string, int64) (threads.Notification, error)
	ClaimPush(context.Context) (*Delivery, error)
	FinishPush(context.Context, Delivery, int) error
}
type Delivery struct {
	ID, Lease, SubscriptionID, NotificationID string
	Revision                                  int64
	Subscription                              Subscription
}

// Derive a separate VAPID signing key from the protected installation key. Its
// domain is independent from the encryption key and stable across restarts.
func VAPID(secret []byte) (private, public string, err error) {
	for counter := byte(0); ; counter++ {
		h := hmac.New(sha256.New, secret)
		h.Write([]byte("acta2:webpush:v1"))
		h.Write([]byte{counter})
		raw := h.Sum(nil)
		key, e := ecdh.P256().NewPrivateKey(raw)
		if e != nil {
			continue
		}
		return base64.RawURLEncoding.EncodeToString(raw), base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()), nil
	}
}
func Validate(s Subscription) error {
	u, e := url.Parse(s.Endpoint)
	if e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || (u.Port() != "" && u.Port() != "443") || len(s.Endpoint) > 4096 {
		return ErrSubscription
	}
	if ip, err := netip.ParseAddr(u.Hostname()); err == nil && !publicIP(ip) {
		return ErrSubscription
	}
	auth, e := base64.RawURLEncoding.DecodeString(strings.TrimRight(s.Keys.Auth, "="))
	if e != nil || len(auth) != 16 {
		return ErrSubscription
	}
	key, e := base64.RawURLEncoding.DecodeString(strings.TrimRight(s.Keys.P256dh, "="))
	if e != nil {
		return ErrSubscription
	}
	if _, e = ecdh.P256().NewPublicKey(key); e != nil {
		return ErrSubscription
	}
	return nil
}
func publicIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, raw := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001:db8::/32", "64:ff9b::/96", "64:ff9b:1::/48", "fec0::/10", "2002::/16", "2001::/32"} {
		if netip.MustParsePrefix(raw).Contains(ip) {
			return false
		}
	}
	return true
}

// Subscription URLs are untrusted input. Resolve and pin public IPs at dial
// time, retain TLS hostname verification, disable redirects and environment proxies.
func HTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, e := net.SplitHostPort(address)
		if e != nil {
			return nil, e
		}
		ips, e := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if e != nil {
			return nil, e
		}
		if len(ips) == 0 {
			return nil, ErrSubscription
		}
		for _, ip := range ips {
			if !publicIP(ip) {
				return nil, ErrSubscription
			}
		}
		var last error
		for _, ip := range ips {
			c, e := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if e == nil {
				return c, nil
			}
			last = e
		}
		return nil, last
	}
	return &http.Client{Transport: transport, Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

type Sender struct {
	Private, Public, Contact string
	Client                   webpush.HTTPClient
}

func (s Sender) Send(ctx context.Context, d Delivery) (int, error) {
	if err := Validate(d.Subscription); err != nil {
		return 410, err
	}
	payload, _ := json.Marshal(map[string]any{"subscription": d.SubscriptionID, "id": d.NotificationID, "revision": d.Revision})
	sum := sha256.Sum256([]byte(d.NotificationID))
	// Config permits HTTP only for loopback development. VAPID subjects must be
	// HTTPS URLs (or mailto); never let the library interpret an HTTP URL as email.
	contact := strings.Replace(s.Contact, "http://", "https://", 1)
	response, err := webpush.SendNotificationWithContext(ctx, payload, &d.Subscription, &webpush.Options{HTTPClient: s.Client, Subscriber: contact, VAPIDPrivateKey: s.Private, VAPIDPublicKey: s.Public, TTL: 3600, Urgency: webpush.UrgencyNormal, Topic: base64.RawURLEncoding.EncodeToString(sum[:18])})
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	return response.StatusCode, nil
}
func Run(ctx context.Context, store Store, sender Sender) {
	for ctx.Err() == nil {
		d, err := store.ClaimPush(ctx)
		if err != nil && ctx.Err() == nil {
			slog.Error("push queue unavailable")
		}
		if err == nil && d != nil {
			status, sendErr := sender.Send(ctx, *d)
			if ctx.Err() == nil && (sendErr != nil || status < 200 || status >= 300) {
				// Endpoint URLs and provider response bodies can contain credentials.
				slog.Warn("push delivery failed", "delivery_id", d.ID, "status", status)
			}
			if ctx.Err() != nil {
				return
			} // The expiring lease makes this retryable after restart.
			if err = store.FinishPush(ctx, *d, status); err != nil {
				slog.Error("push delivery acknowledgement failed")
			}
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
	}
}
