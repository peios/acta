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
