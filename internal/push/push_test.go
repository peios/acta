package push

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	webpush "github.com/SherClockHolmes/webpush-go"
	"golang.org/x/crypto/hkdf"
	"io"
	"net/http"
	"net/netip"
	"strings"
	"testing"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) Do(r *http.Request) (*http.Response, error) { return f(r) }
func TestEncryptedPushEnvelope(t *testing.T) {
	receiver, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	auth := bytes.Repeat([]byte{3}, 16)
	private, public, err := VAPID(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	p2, u2, _ := VAPID(bytes.Repeat([]byte{7}, 32))
	if p2 != private || u2 != public {
		t.Fatal("VAPID changed across restart")
	}
	derive := func(secret, salt, info []byte, size int) []byte {
		out := make([]byte, size)
		if _, err := io.ReadFull(hkdf.New(sha256.New, secret, salt, info), out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	sender := Sender{Private: private, Public: public, Contact: "https://acta.example", Client: transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme != "https" || r.Header.Get("Content-Encoding") != "aes128gcm" || !strings.Contains(r.Header.Get("Authorization"), "vapid ") || r.Header.Get("Topic") == "" {
			t.Fatal("missing encrypted push headers")
		}
		body, _ := io.ReadAll(r.Body)
		if bytes.Contains(body, []byte("notification")) {
			t.Fatal("plaintext payload")
		}
		salt, pub, encrypted := body[:16], body[21:86], body[86:]
		key, err := ecdh.P256().NewPublicKey(pub)
		if err != nil {
			t.Fatal(err)
		}
		shared, err := receiver.ECDH(key)
		if err != nil {
			t.Fatal(err)
		}
		info := append(append([]byte("WebPush: info\x00"), receiver.PublicKey().Bytes()...), pub...)
		ikm := derive(shared, auth, info, 32)
		block, err := aes.NewCipher(derive(ikm, salt, []byte("Content-Encoding: aes128gcm\x00"), 16))
		if err != nil {
			t.Fatal(err)
		}
		gcm, _ := cipher.NewGCM(block)
		plain, err := gcm.Open(nil, derive(ikm, salt, []byte("Content-Encoding: nonce\x00"), 12), encrypted, nil)
		if err != nil {
			t.Fatal(err)
		}
		plain = bytes.TrimRight(plain, "\x00")
		plain = bytes.TrimSuffix(plain, []byte{2})
		var result map[string]any
		if err = json.Unmarshal(plain, &result); err != nil {
			t.Fatal(err)
		}
		if len(result) != 3 || result["id"] != "notification" || result["subscription"] != "subscription" || result["revision"] != float64(4) {
			t.Fatal(result)
		}
		return &http.Response{StatusCode: 201, Body: io.NopCloser(strings.NewReader(""))}, nil
	})}
	status, err := sender.Send(context.Background(), Delivery{NotificationID: "notification", SubscriptionID: "subscription", Revision: 4, Subscription: Subscription{Endpoint: "https://push.example/endpoint", Keys: webpush.Keys{Auth: base64.RawURLEncoding.EncodeToString(auth), P256dh: base64.RawURLEncoding.EncodeToString(receiver.PublicKey().Bytes())}}})
	if err != nil || status != 201 {
		t.Fatal(status, err)
	}
}
func TestPushRejectsPrivateAddresses(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "::1", "::ffff:127.0.0.1", "10.0.0.1", "192.168.1.4", "169.254.169.254", "100.64.0.1", "0.0.0.0", "224.0.0.1", "fc00::1", "fe80::1", "2001:db8::1"} {
		if publicIP(netip.MustParseAddr(ip)) {
			t.Errorf("accepted %s", ip)
		}
	}
	if !publicIP(netip.MustParseAddr("8.8.8.8")) {
		t.Fatal("public address rejected")
	}
	client := HTTPClient()
	if _, err := client.Get("https://127.0.0.1/metadata"); err == nil {
		t.Fatal("allowed loopback fetch")
	}
	if client.CheckRedirect(&http.Request{}, nil) != http.ErrUseLastResponse {
		t.Fatal("follows redirects")
	}
}
