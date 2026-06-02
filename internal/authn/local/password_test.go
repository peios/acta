package local

import "testing"

func TestHashVerifyRoundtrip(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}

	ok, err := VerifyPassword(hash, "correct horse battery staple")
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !ok {
		t.Fatal("expected correct password to verify")
	}

	ok, err = VerifyPassword(hash, "wrong password")
	if err != nil {
		t.Fatalf("verify wrong: %v", err)
	}
	if ok {
		t.Fatal("expected wrong password to fail")
	}
}

func TestHashIsSalted(t *testing.T) {
	a, _ := HashPassword("same")
	b, _ := HashPassword("same")
	if a == b {
		t.Fatal("two hashes of the same password should differ (random salt)")
	}
}

func TestVerifyInvalidHash(t *testing.T) {
	for _, bad := range []string{"", "plaintext", "$argon2id$bogus", "$bcrypt$x$y$z$w$v"} {
		if _, err := VerifyPassword(bad, "x"); err == nil {
			t.Fatalf("expected error for malformed hash %q", bad)
		}
	}
}

func TestValidatePassword(t *testing.T) {
	for _, bad := range []string{"", "short", "1234567"} { // all < 8
		if err := ValidatePassword(bad); err == nil {
			t.Errorf("expected %q to be rejected", bad)
		}
	}
	for _, good := range []string{"12345678", "correct horse", "πππππππππ"} { // >= 8 runes
		if err := ValidatePassword(good); err != nil {
			t.Errorf("expected %q to be accepted, got %v", good, err)
		}
	}
}
