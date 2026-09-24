// Package codehosts defines private host registration and presence contracts.
// It does not execute agents or provide access to a host's filesystem.
package codehosts

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"acta/internal/accounts"
	"github.com/google/uuid"
)

const HeartbeatInterval = 10 * time.Second
const LeaseLifetime = 30 * time.Second

var ErrInUse = errors.New("This host is already running. Stop the other instance or wait for its presence to expire.")

type Host struct {
	ProviderStatus *ProviderReport `json:"provider_status,omitempty"`
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	OS             string          `json:"os"`
	Arch           string          `json:"arch"`
	Online         bool            `json:"online"`
	Connected      bool            `json:"connected"`
	LastSeenAt     time.Time       `json:"last_seen_at"`
}

type Heartbeat struct {
	ID         string `json:"id"`
	InstanceID string `json:"instance_id"`
	Name       string `json:"name"`
	OS         string `json:"os"`
	Arch       string `json:"arch"`
}

func ValidateIDs(id, instance string) error {
	for _, v := range []string{id, instance} {
		parsed, err := uuid.Parse(v)
		if err != nil || parsed == uuid.Nil || parsed.String() != v {
			return &accounts.FieldError{Field: "id", Message: "Use canonical, non-empty host and instance UUIDs."}
		}
	}
	return nil
}

func (h Heartbeat) Validate() error {
	if err := ValidateIDs(h.ID, h.InstanceID); err != nil {
		return err
	}
	for field, value := range map[string]string{"name": h.Name, "os": h.OS, "arch": h.Arch} {
		limit := 32
		if field == "name" {
			limit = 100
		}
		if value == "" || value != strings.TrimSpace(value) || !utf8.ValidString(value) || len(value) > limit || strings.IndexFunc(value, func(r rune) bool {
			return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == '\u2028' || r == '\u2029'
		}) >= 0 {
			return &accounts.FieldError{Field: field, Message: "Use a short, non-empty host label without control characters."}
		}
	}
	return nil
}

// Store must scope every operation by the authenticated human owner. Renew is
// atomic: a different live instance cannot take over, while retrying the same
// instance is idempotent. Release must not affect a newer instance. Presence
// also requires a live issuing session; revocation makes it offline immediately.
type Store interface {
	RenewCodeHost(context.Context, string, []byte, Heartbeat, time.Time) (Host, error)
	ReleaseCodeHost(context.Context, string, []byte, string, string) error
	ListCodeHosts(context.Context, string, time.Time) ([]Host, error)
}
