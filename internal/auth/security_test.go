package auth

import (
	"bytes"
	"path/filepath"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
)

func TestAuthenticationPolicyMatrix(t *testing.T) {
	now := time.Now()
	for _, mfa := range []bool{false, true} {
		for _, extra := range []bool{false, true} {
			for _, method := range []string{"password", "passkey"} {
				for _, code := range []bool{false, true} {
					d := SecurityData{ExtraCode: extra, Passkeys: []Passkey{{ID: "key"}}}
					if mfa {
						d.Secret = "secret"
					}
					p := Proof{Method: method, CredentialID: "key", At: now, Version: 3, MFA: code}
					want := !mfa || method == "passkey" && !extra || code
					if got := proofValid(d, p, 3, now); got != want {
						t.Fatalf("mfa=%v extra=%v method=%s code=%v: %v", mfa, extra, method, code, got)
					}
					if proofValid(d, p, 4, now) || proofValid(d, p, 3, now.Add(FreshLifetime)) {
						t.Fatal("stale proof accepted")
					}
				}
			}
		}
	}
	if proofValid(SecurityData{}, Proof{Method: "passkey", CredentialID: "removed", At: now, Version: 3}, 3, now) {
		t.Fatal("removed passkey still supplies fresh proof")
	}
}
func TestTOTPReplayAndWindow(t *testing.T) {
	secret := "JBSWY3DPEHPK3PXP"
	now := time.Unix(1800000000, 0)
	code, err := totp.GenerateCode(secret, now)
	if err != nil {
		t.Fatal(err)
	}
	step, ok := validTOTPStep(secret, code, now, 0)
	if !ok {
		t.Fatal("valid code rejected")
	}
	if _, ok = validTOTPStep(secret, code, now, step); ok {
		t.Fatal("code replay accepted")
	}
	if _, ok = validTOTPStep(secret, code, now.Add(2*time.Minute), 0); ok {
		t.Fatal("expired code accepted")
	}
}
func TestSecurityEncryptionAndKeyPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "key")
	key, err := LoadSecurityKey(path)
	if err != nil {
		t.Fatal(err)
	}
	again, err := LoadSecurityKey(path)
	if err != nil || !bytes.Equal(key, again) {
		t.Fatal("key did not persist", err)
	}
	a, err := newCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := seal(a, "account:one", SecurityData{Secret: "sensitive"})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("sensitive")) {
		t.Fatal("secret stored in plaintext")
	}
	var d SecurityData
	if err = unseal(a, "account:two", raw, &d); err == nil {
		t.Fatal("ciphertext accepted in a different account context")
	}
	if err = unseal(a, "account:one", raw, &d); err != nil || d.Secret != "sensitive" {
		t.Fatal("decrypt failed", err)
	}
}
