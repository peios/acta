// Package client is the HTTP boundary shared by CLI commands. It has no server
// implementation dependencies and never follows redirects with credentials.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	URL, Token string
	HTTP       *http.Client
}
type Error struct {
	Status  int               `json:"status"`
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
	Current json.RawMessage   `json:"current,omitempty"`
}

func (e *Error) Error() string { return e.Message }
func NormalizeURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if !strings.Contains(raw, "://") {
		host := strings.Split(raw, "/")[0]
		name := host
		if h, _, e := net.SplitHostPort(host); e == nil {
			name = h
		}
		scheme := "https://"
		if name == "localhost" || net.ParseIP(strings.Trim(name, "[]")).IsLoopback() {
			scheme = "http://"
		}
		raw = scheme + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", errors.New("Use a server URL such as https://acta.example.org, without a path, credentials or query")
	}
	u.Host = strings.ToLower(u.Host)
	u.Path = ""
	loop := u.Hostname() == "localhost" || net.ParseIP(u.Hostname()).IsLoopback()
	if u.Scheme != "https" && !(u.Scheme == "http" && loop) {
		return "", errors.New("HTTPS is required except for loopback development servers")
	}
	if (u.Scheme == "https" && u.Port() == "443") || (u.Scheme == "http" && u.Port() == "80") {
		u.Host = u.Hostname()
		if strings.Contains(u.Host, ":") {
			u.Host = "[" + u.Host + "]"
		}
	}
	return u.String(), nil
}
func New(server, token string) *Client {
	return &Client{URL: server, Token: token, HTTP: &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (c *Client) Call(ctx context.Context, method, path string, input, output any) error {
	server, err := NormalizeURL(c.URL)
	if err != nil {
		return err
	}
	var body io.Reader
	if input != nil {
		raw, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, server+"/api/"+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "acta2-cli")
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("connect to Acta: %w", err)
	}
	defer resp.Body.Close()
	const responseLimit = 8 * 1024 * 1024
	raw, err := io.ReadAll(io.LimitReader(resp.Body, responseLimit+1))
	if err != nil {
		return err
	}
	if len(raw) > responseLimit {
		return errors.New("Acta response is too large")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var result struct{ Error Error }
		_ = json.Unmarshal(raw, &result)
		if result.Error.Message == "" {
			result.Error.Message = fmt.Sprintf("Acta returned HTTP %d", resp.StatusCode)
		}
		result.Error.Status = resp.StatusCode
		return &result.Error
	}
	if output != nil {
		if err = json.Unmarshal(raw, output); err != nil {
			return errors.New("Acta returned an invalid JSON response")
		}
	}
	return nil
}

type Account struct {
	ID          string  `json:"id"`
	Username    string  `json:"username"`
	DisplayName *string `json:"display_name"`
	MFARequired bool    `json:"mfa_setup_required"`
}

func (c *Client) Account(ctx context.Context) (Account, error) {
	var a Account
	err := c.Call(ctx, "GET", "account", nil, &a)
	return a, err
}
func (c *Client) Logout(ctx context.Context) error {
	return c.Call(ctx, "POST", "logout", struct{}{}, nil)
}

type Device struct {
	DeviceCode string `json:"device_code"`
	UserCode   string `json:"user_code"`
	ExpiresIn  int    `json:"expires_in"`
	Interval   int    `json:"interval"`
}

func (c *Client) Start(ctx context.Context, machine string) (Device, error) {
	var d Device
	err := c.Call(ctx, "POST", "device/start", map[string]string{"machine": machine}, &d)
	return d, err
}
func (c *Client) Wait(ctx context.Context, d Device) (string, error) {
	if d.Interval < 1 || d.ExpiresIn < 1 || d.ExpiresIn > 3600 {
		return "", errors.New("Invalid device login response")
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(d.ExpiresIn)*time.Second)
	defer cancel()
	delay := time.Duration(d.Interval) * time.Second
	for {
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return "", fmt.Errorf("login ended: %w", ctx.Err())
		case <-timer.C:
		}
		var r struct{ Status, Token string }
		err := c.Call(ctx, "POST", "device/poll", map[string]string{"device_code": d.DeviceCode}, &r)
		var apiErr *Error
		if errors.As(err, &apiErr) && apiErr.Status == 429 {
			delay += 5 * time.Second
			continue
		}
		if err != nil {
			return "", err
		}
		switch r.Status {
		case "pending":
			continue
		case "denied":
			return "", errors.New("CLI access was declined in the browser")
		case "approved":
			if !strings.HasPrefix(r.Token, "cli_") {
				return "", errors.New("Invalid CLI credential response")
			}
			return r.Token, nil
		default:
			return "", errors.New("Unexpected device login response")
		}
	}
}
