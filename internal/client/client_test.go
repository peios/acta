package client

import (
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

func TestNormalizeURL(t *testing.T) {
	good := map[string]string{"localhost:8081": "http://localhost:8081", "ACTA.example.org/": "https://acta.example.org", "https://acta.example.org:443": "https://acta.example.org", "[::1]:8081": "http://[::1]:8081"}
	for in, want := range good {
		got, e := NormalizeURL(in)
		if e != nil || got != want {
			t.Fatalf("%q = %q, %v", in, got, e)
		}
	}
	for _, in := range []string{"http://acta.example.org", "https://user:pass@acta.example.org", "https://acta.example.org/path", "https://acta.example.org?token=secret", "https://acta.example.org#fragment", ""} {
		if _, e := NormalizeURL(in); e == nil {
			t.Fatalf("accepted %q", in)
		}
	}
}

type redirectTransport struct{ calls atomic.Int32 }

func (t *redirectTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	t.calls.Add(1)
	return &http.Response{StatusCode: 307, Header: http.Header{"Location": []string{"https://elsewhere.example.org"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
}
func TestCredentialsNeverFollowRedirect(t *testing.T) {
	transport := &redirectTransport{}
	c := New("https://acta.example.org", "cli_secret")
	c.HTTP.Transport = transport
	_, e := c.Account(t.Context())
	if e == nil || transport.calls.Load() != 1 {
		t.Fatal("followed credential redirect", e)
	}
}
