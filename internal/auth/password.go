package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
	"golang.org/x/text/unicode/norm"
)

const PasswordMin = 15
const PasswordMax = 128

// OWASP's Argon2id minimum profile. Concurrent hashes are separately bounded.
const memoryKiB = 19 * 1024
const iterations = 2
const parallelism = 1

//go:embed data/common-passwords.txt data/LICENSE-SecLists.txt
var passwordData embed.FS

func init() {
	data, err := passwordData.ReadFile("data/common-passwords.txt")
	if err != nil {
		panic(err)
	} // An embedded build input, never a runtime dependency.
	for _, word := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		commonPasswords[word] = true
	}
}

// Supplement the bundled common-password list; see data/README.md for provenance.
var commonPasswords = map[string]bool{
	"passwordpassword": true, "passwordpasswordpassword": true,
	"123456789012345": true, "1234567890123456": true,
	"12345678901234567890": true, "qwertyuiopasdfgh": true,
	"qwertyuiopasdfghjkl": true, "qwertyuiop123456": true,
	"letmeinletmeinletmein": true, "iloveyouiloveyou": true,
	"aaaaaaaaaaaaaaa": true, "password123456789": true,
}

func NormalizePassword(raw string) (string, error) {
	if !utf8.ValidString(raw) {
		return "", errors.New("Enter a valid password.")
	}
	return norm.NFC.String(raw), nil
}

func ValidatePassword(raw string) (string, error) {
	password, err := NormalizePassword(raw)
	if err != nil {
		return "", err
	}
	length := utf8.RuneCountInString(password)
	if length < PasswordMin || length > PasswordMax {
		return "", fmt.Errorf("Use between %d and %d characters for your password.", PasswordMin, PasswordMax)
	}
	// Do not trim or alter the accepted password. Trim only for the blocklist.
	candidate := strings.ToLower(strings.TrimSpace(password))
	repeated := true
	var first rune
	for i, r := range password {
		if i == 0 {
			first = r
		}
		if r != first {
			repeated = false
		}
	}
	if candidate == "" || repeated || commonPasswords[candidate] {
		return "", errors.New("This password is too common. Choose a different passphrase or a generated password.")
	}
	return password, nil
}

func HashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, iterations, memoryKiB, parallelism, 32)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", memoryKiB, iterations, parallelism, base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

func VerifyPassword(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" || parts[2] != "v=19" {
		return false
	}
	var memory, rounds uint32
	var lanes uint8
	if n, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &rounds, &lanes); err != nil || n != 3 {
		return false
	}
	if parts[3] != fmt.Sprintf("m=%d,t=%d,p=%d", memory, rounds, lanes) {
		return false
	}
	// Bound even database-supplied parameters before allocating memory.
	if memory < 8*1024 || memory > 64*1024 || rounds < 1 || rounds > 5 || lanes < 1 || lanes > 4 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) != 16 {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(want) != 32 {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, rounds, memory, lanes, uint32(len(want)))
	return subtle.ConstantTimeCompare(want, got) == 1
}
