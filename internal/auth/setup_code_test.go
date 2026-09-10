package auth

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestSetupCodeFormatAndSessionTokenSeparation(t *testing.T) {
	code, err := newSetupCode()
	if err != nil {
		t.Fatal(err)
	}
	if len(code) != 8 {
		t.Fatalf("setup code length: %d", len(code))
	}
	for _, symbol := range code {
		if !strings.ContainsRune(setupCodeAlphabet, symbol) {
			t.Fatalf("unexpected setup symbol %q", symbol)
		}
	}
	// Human-friendly setup codes must never shorten session or setup-grant tokens.
	token, err := Token()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != 32 {
		t.Fatalf("token no longer has 32 random bytes: %v", err)
	}
}
