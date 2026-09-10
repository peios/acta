package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base32"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

// LoadSecurityKey provisions a private local key once. Back it up alongside the
// database. A database fingerprint detects accidental replacement at startup.
func LoadSecurityKey(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return nil, err
		}
		key := make([]byte, 32)
		if _, err = rand.Read(key); err != nil {
			return nil, err
		}
		f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if errors.Is(e, os.ErrExist) {
			return LoadSecurityKey(path)
		}
		if e != nil {
			return nil, e
		}
		_, err = f.WriteString(base64.RawStdEncoding.EncodeToString(key))
		if err == nil {
			err = f.Sync()
		}
		closeErr := f.Close()
		if err != nil {
			return nil, err
		}
		if closeErr != nil {
			return nil, closeErr
		}
		return key, nil
	}
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("security key file must be private (chmod 600)")
	}
	key, err := base64.RawStdEncoding.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil || len(key) != 32 {
		return nil, errors.New("security key file must contain a base64-encoded 32-byte key")
	}
	return key, nil
}
func newCipher(key []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, errors.New("security encryption requires a 32-byte key")
	}
	b, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(b)
}

// VerifyRecoverySecurity validates account decryption without creating flows,
// sessions, or mutations in a recovered installation.
func VerifyRecoverySecurity(key []byte, id string, raw []byte) error {
	c, err := newCipher(key)
	if err != nil {
		return err
	}
	var data SecurityData
	return unseal(c, "account:"+id, raw, &data)
}
func seal(a cipher.AEAD, context string, value any) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, a.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	return a.Seal(nonce, nonce, raw, []byte(context)), nil
}
func unseal(a cipher.AEAD, context string, raw []byte, value any) error {
	if len(raw) < a.NonceSize() {
		return errors.New("invalid encrypted security state")
	}
	plain, err := a.Open(nil, raw[:a.NonceSize()], raw[a.NonceSize():], []byte(context))
	if err != nil {
		return errors.New("cannot decrypt security state")
	}
	return json.Unmarshal(plain, value)
}
func recoveryCodes() ([]string, []string, error) {
	codes := []string{}
	hashes := []string{}
	for range 10 {
		raw := make([]byte, 10)
		if _, err := rand.Read(raw); err != nil {
			return nil, nil, err
		}
		s := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw)
		code := fmt.Sprintf("%s-%s-%s-%s", s[:4], s[4:8], s[8:12], s[12:])
		codes = append(codes, code)
		hashes = append(hashes, recoveryDigest(code))
	}
	return codes, hashes, nil
}
func recoveryDigest(code string) string {
	return base64.RawURLEncoding.EncodeToString(Digest(strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(code), "-", ""))))
}
func validTOTPStep(secret, code string, now time.Time, last int64) (int64, bool) {
	if len(code) != 6 {
		return 0, false
	}
	for _, delta := range []int64{0, -1, 1} {
		step := now.Unix()/30 + delta
		if step <= last {
			continue
		}
		ok, err := totp.ValidateCustom(code, secret, time.Unix(step*30, 0), totp.ValidateOpts{Period: 30, Skew: 0, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1})
		if err == nil && ok {
			return step, true
		}
	}
	return 0, false
}
