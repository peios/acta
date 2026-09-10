package client

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"syscall"
	"testing"
)

type accountTransport func(*http.Request) (*http.Response, error)

func (f accountTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestHarnessAccountAvailabilityRetry(t *testing.T) {
	for _, failure := range []string{"connection refused", "unavailable", "rate limited"} {
		t.Run(failure, func(t *testing.T) {
			calls, reports := 0, 0
			c := New("http://localhost:8081", "test")
			c.HTTP.Transport = accountTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					if failure == "connection refused" {
						return nil, &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}
					}
					status := 503
					if failure == "rate limited" {
						status = 429
					}
					return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("{}"))}, nil
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"id":"owner","username":"jack"}`))}, nil
			})
			account, err := c.HarnessAccount(t.Context(), "host", func(s HarnessState) error {
				reports++
				if s.State != "reconnecting" || s.RetryIn <= 0 {
					t.Fatal(s)
				}
				return nil
			})
			if err != nil || account.ID != "owner" || calls != 2 || reports != 1 {
				t.Fatalf("%+v %v calls=%d reports=%d", account, err, calls, reports)
			}
		})
	}
}
func TestHarnessAccountTerminalFailures(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
	}{{401, "{}"}, {403, "{}"}, {302, "{}"}, {200, "broken"}, {200, "{}"}} {
		c := New("http://localhost:8081", "test")
		c.HTTP.Transport = accountTransport(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
		})
		if _, err := c.HarnessAccount(t.Context(), "host", func(HarnessState) error { t.Fatal("retried terminal failure"); return nil }); err == nil {
			t.Fatal("accepted bad response")
		}
	}
}
func TestHarnessAccountCancellation(t *testing.T) {
	c := New("http://localhost:8081", "test")
	c.HTTP.Transport = accountTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader("{}"))}, nil
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	_, err := c.HarnessAccount(ctx, "host", func(HarnessState) error { cancel(); return nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
