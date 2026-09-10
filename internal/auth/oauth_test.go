package auth

import "testing"

func TestOAuthRedirects(t *testing.T) {
	for _, uri := range []string{"https://client.example/callback", "http://127.0.0.1:2345/callback", "http://[::1]:2345/callback", "http://localhost:8080/callback"} {
		if err := ValidateRedirect(uri); err != nil {
			t.Fatal(uri, err)
		}
	}
	for _, uri := range []string{"//evil.example/callback", "http://evil.example/callback", "javascript:alert(1)", "https://user:pass@client.example/callback", "https://client.example/callback#fragment", "/callback"} {
		if ValidateRedirect(uri) == nil {
			t.Fatal("accepted", uri)
		}
	}
	for _, pair := range [][2]string{{"http://127.0.0.1:123/callback", "http://127.0.0.1:456/callback"}, {"http://[::1]:123/callback", "http://[::1]:456/callback"}} {
		if !RedirectMatches(pair[0], pair[1]) {
			t.Fatal("rejected loopback port", pair)
		}
	}
	for _, pair := range [][2]string{{"https://client.example:123/callback", "https://client.example:456/callback"}, {"http://127.0.0.1:123/callback", "http://127.0.0.1:456/other"}, {"http://localhost:123/callback", "http://localhost:456/callback"}, {"https://client.example/callback", "https://evil.example/callback"}} {
		if RedirectMatches(pair[0], pair[1]) {
			t.Fatal("accepted unregistered callback", pair)
		}
	}
}
