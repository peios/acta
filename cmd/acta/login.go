package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// cmdLogin runs the gh-style loopback flow: start a local listener, open the
// browser to the server's authorize page, and receive the minted token back.
func cmdLogin(args []string) error {
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	base, err := normalizeBase(fs.Arg(0))
	if err != nil {
		return err
	}
	token, err := authorize(base, tokenLabel())
	if err != nil {
		return err
	}
	if err := saveConfig(config{URL: base, Token: token}); err != nil {
		return err
	}
	c := &client{base: base, token: token, hc: &http.Client{Timeout: 15 * time.Second}}
	if data, err := c.do("GET", "/api/v1/me", nil); err == nil {
		var me struct{ Username string }
		_ = json.Unmarshal(data, &me)
		fmt.Printf("Logged in to %s as %s\n", base, me.Username)
	} else {
		fmt.Printf("Logged in to %s\n", base)
	}
	return nil
}

// authorize runs the gh-style loopback flow against base and returns the minted
// token plaintext. label names the token in the account's token list. It opens
// the browser, serves a one-shot 127.0.0.1 callback, and waits for the redirect.
func authorize(base, label string) (string, error) {
	// Local listener that the browser is redirected back to.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	defer ln.Close()
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback", ln.Addr().(*net.TCPAddr).Port)

	state, err := randString()
	if err != nil {
		return "", err
	}

	type result struct {
		token string
		err   error
	}
	resCh := make(chan result, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch {
		case q.Get("state") != state:
			callbackPage(w, "Authorization failed (state mismatch). You can close this tab.")
			resCh <- result{err: fmt.Errorf("state mismatch — did not initiate this login")}
		case q.Get("error") != "":
			callbackPage(w, "Authorization was cancelled. You can close this tab.")
			resCh <- result{err: fmt.Errorf("authorization denied")}
		case q.Get("token") == "":
			callbackPage(w, "Authorization failed. You can close this tab.")
			resCh <- result{err: fmt.Errorf("no token returned")}
		default:
			callbackPage(w, "Logged in to Acta. You can close this tab and return to the terminal.")
			resCh <- result{token: q.Get("token")}
		}
	})
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	defer srv.Close()

	authURL := base + "/cli/authorize?" + url.Values{
		"redirect_uri": {redirectURI},
		"state":        {state},
		"label":        {label},
	}.Encode()
	fmt.Fprintf(os.Stderr, "Opening your browser to authorize. If it doesn't open, visit:\n  %s\n\n", authURL)
	_ = openBrowser(authURL)

	select {
	case res := <-resCh:
		return res.token, res.err
	case <-time.After(3 * time.Minute):
		return "", fmt.Errorf("timed out waiting for authorization")
	}
}

// cmdLogout revokes the token `acta login` stored and forgets it. It leaves any
// ACTA_TOKEN in the environment alone — logout undoes login, not the env.
func cmdLogout([]string) error {
	cfg := loadConfig()
	if cfg.Token == "" {
		fmt.Println("Not logged in (no stored credentials).")
		return nil
	}
	base := cfg.URL
	if base == "" {
		base = "http://localhost:8080"
	}
	c := &client{base: strings.TrimRight(base, "/"), token: cfg.Token, hc: &http.Client{Timeout: 15 * time.Second}}
	_, _ = c.do("POST", "/api/v1/logout", nil) // best-effort revoke
	if err := clearConfig(); err != nil {
		return err
	}
	fmt.Println("Logged out.")
	return nil
}

// tokenLabel names the token after this machine, so it's identifiable in the
// account's token list ("acta CLI @ jacks-laptop").
func tokenLabel() string {
	if h, err := os.Hostname(); err == nil && h != "" {
		return "acta CLI @ " + h
	}
	return "acta CLI"
}

// normalizeBase turns a host argument (or ACTA_URL, or the default) into a base
// URL. A bare host defaults to https, except loopback which defaults to http.
func normalizeBase(host string) (string, error) {
	if host == "" {
		host = os.Getenv("ACTA_URL")
	}
	if host == "" {
		host = "http://localhost:8080"
	}
	if !strings.Contains(host, "://") {
		scheme := "https"
		if isLocalHost(host) {
			scheme = "http"
		}
		host = scheme + "://" + host
	}
	u, err := url.Parse(host)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("invalid host %q", host)
	}
	return strings.TrimRight(u.Scheme+"://"+u.Host, "/"), nil
}

func isLocalHost(host string) bool {
	h := host
	if i := strings.IndexByte(h, ':'); i >= 0 && !strings.HasPrefix(h, "[") {
		h = h[:i]
	}
	return h == "localhost" || h == "127.0.0.1" || strings.HasPrefix(host, "[::1]")
}

func openBrowser(target string) error {
	var name string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		name, args = "open", []string{target}
	case "windows":
		name, args = "rundll32", []string{"url.dll,FileProtocolHandler", target}
	default:
		name, args = "xdg-open", []string{target}
	}
	return exec.Command(name, args...).Start()
}

func randString() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// callbackPage renders the browser's "you can close this tab" page and scrubs
// the token out of the URL bar / history. This is the CLI's own loopback
// server, so an inline script is fine (no CSP here).
func callbackPage(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!doctype html><html><head><meta charset="utf-8"><title>Acta CLI</title>
<style>body{font-family:system-ui,sans-serif;background:#0a0908;color:#edeae4;display:grid;place-items:center;height:100vh;margin:0}
div{max-width:30rem;text-align:center;padding:2rem}h2{color:#e8995a}</style>
<script>history.replaceState(null,"","/callback")</script></head>
<body><div><h2>Acta CLI</h2><p>%s</p></div></body></html>`, msg)
}
