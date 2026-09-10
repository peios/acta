package auth

import (
	"strings"
	"testing"
)

func TestPasswordPolicy(t *testing.T) {
	for _, tc := range []struct {
		raw   string
		valid bool
	}{
		{"silver meadow orbit lamp", true}, {"  spaces stay here  ", true}, {strings.Repeat("ab", 64), true}, {"éclair orbit violet", true},
		{"short", false}, {strings.Repeat("a", 15), false}, {strings.Repeat(" ", 20), false}, {strings.Repeat("ab", 65), false}, {"PasswordPassword", false}, {"12345678901234567890", false}, {"\xffsilver meadow orbit", false},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			p, err := ValidatePassword(tc.raw)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v err=%v", tc.valid, err)
			}
			if tc.valid && p != tc.raw {
				t.Fatal("unexpected transformation")
			}
		})
	}
	p, err := ValidatePassword("e\u0301clair orbit violet")
	if err != nil || p != "éclair orbit violet" {
		t.Fatalf("NFC: %q %v", p, err)
	}
	if len(commonPasswords) < 70 {
		t.Fatal("embedded blocklist missing")
	}
}
func TestPasswordHash(t *testing.T) {
	password := "silver meadow orbit lamp"
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	other, err := HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	if hash == other {
		t.Fatal("salt reused")
	}
	if !VerifyPassword(password, hash) || VerifyPassword("wrong password", hash) {
		t.Fatal("verification mismatch")
	}
	for _, bad := range []string{"", "hash", strings.Replace(hash, "v=19", "v=18", 1), strings.Replace(hash, "m=19456", "m=999999999", 1), strings.Replace(hash, "p=1", "p=0", 1), strings.Replace(hash, "p=1", "p=1junk", 1), hash + "$extra", strings.Replace(hash, "$argon2id$", "x$argon2id$", 1)} {
		if VerifyPassword(password, bad) {
			t.Fatalf("accepted malformed hash %s", bad)
		}
	}
}
