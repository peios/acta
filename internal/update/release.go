// Package update owns the signed release contract and durable deployment updater.
package update

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

const Protocol = 1

// Layout 2 uses the final Acta executable, database and backup-stanza names.
const Layout = 2

type Release struct {
	Version       string            `json:"version"`
	Sequence      int64             `json:"sequence"`
	Repository    string            `json:"repository"`
	Protocol      int               `json:"updater_protocol"`
	Layout        int               `json:"deployment_layout"`
	Postgres      int               `json:"postgres_major"`
	Schema        int               `json:"schema"`
	MinimumSchema int               `json:"minimum_schema"`
	Notes         string            `json:"notes"`
	Images        map[string]string `json:"images"`
}
type Signed struct {
	Payload   string `json:"payload"`
	Signature string `json:"signature"`
}

var repositoryPattern = regexp.MustCompile(`^[a-zA-Z0-9_.-]+/[a-zA-Z0-9_.-]+$`)
var versionPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(?:-[a-zA-Z0-9.-]+)?$`)
var digestPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9./:_-]*@sha256:[a-f0-9]{64}$`)

func (r Release) Validate() error {
	if !repositoryPattern.MatchString(r.Repository) || !versionPattern.MatchString(r.Version) || r.Sequence <= 0 || len(r.Notes) > 64000 {
		return errors.New("invalid release identity")
	}
	if r.Protocol != Protocol || r.Layout != Layout {
		return errors.New("this release requires an operator upgrade of the updater or deployment layout")
	}
	if r.Postgres != 17 || r.Schema < 40 || r.MinimumSchema < 40 || r.MinimumSchema > r.Schema {
		return errors.New("unsupported database compatibility")
	}
	if len(r.Images) != 5 {
		return errors.New("release must include app, db, backup, caddy and updater images")
	}
	for _, name := range []string{"app", "db", "backup", "caddy", "updater"} {
		image := r.Images[name]
		if !digestPattern.MatchString(image) {
			return fmt.Errorf("%s image must use an immutable digest", name)
		}
		packageName := name
		if name == "app" {
			packageName = "server"
		}
		prefix := "ghcr.io/" + strings.ToLower(r.Repository) + "-" + packageName + "@"
		if name == "caddy" {
			prefix = "caddy@"
		}
		if !strings.HasPrefix(image, prefix) {
			return fmt.Errorf("%s image is outside the release namespace", name)
		}
	}
	return nil
}
func Sign(r Release, seed []byte) (Signed, error) {
	if err := r.Validate(); err != nil {
		return Signed{}, err
	}
	if len(seed) != ed25519.SeedSize {
		return Signed{}, errors.New("signing seed must be 32 bytes")
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return Signed{}, err
	}
	return Signed{base64.StdEncoding.EncodeToString(raw), base64.StdEncoding.EncodeToString(ed25519.Sign(ed25519.NewKeyFromSeed(seed), raw))}, nil
}
func Verify(s Signed, public []byte, repository string) (Release, error) {
	var r Release
	raw, e := base64.StdEncoding.DecodeString(s.Payload)
	if e != nil || len(raw) > 128<<10 {
		return r, errors.New("invalid release payload")
	}
	sig, e := base64.StdEncoding.DecodeString(s.Signature)
	if e != nil || len(public) != ed25519.PublicKeySize || !ed25519.Verify(public, raw, sig) {
		return r, errors.New("release signature verification failed")
	}
	if e = json.Unmarshal(raw, &r); e != nil {
		return r, e
	}
	if e = r.Validate(); e != nil {
		return r, e
	}
	if r.Repository != repository {
		return r, errors.New("release repository does not match installation")
	}
	return r, nil
}
func (s Signed) ID() string {
	sum := sha256.Sum256([]byte(s.Payload))
	return hex.EncodeToString(sum[:])
}
func Compatible(current, next Release) error {
	if next.Sequence <= current.Sequence {
		return errors.New("release is not newer than the installed release")
	}
	if current.Schema < next.MinimumSchema || current.Schema > next.Schema {
		return errors.New("release does not support this installed database schema")
	}
	if current.Postgres != next.Postgres {
		return errors.New("PostgreSQL major upgrades require a separate recovery procedure")
	}
	return nil
}
