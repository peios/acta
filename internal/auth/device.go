package auth

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"

	"acta2/internal/accounts"
	"github.com/google/uuid"
)

var ErrDevice = errors.New("This device code is invalid, expired or already used. Start login again in the CLI.")

const DeviceLifetime = 10 * time.Minute

type DeviceRequest struct {
	Digest, CodeDigest            []byte
	Description, AccountID, State string
	ExpiresAt                     time.Time
	Encrypted                     []byte
}

// WithDevice locks the active account before the request. Session creation and
// approval/consumption commit atomically; callbacks must recheck live policy.
type DeviceStore interface {
	CreateDevice(context.Context, DeviceRequest) error
	ReadDevice(context.Context, []byte, bool) (DeviceRequest, error)
	WithDevice(context.Context, string, []byte, func(*SecurityRecord, SecurityTx, *DeviceRequest) error) error
}
type DeviceStart struct {
	DeviceCode string `json:"device_code"`
	UserCode   string `json:"user_code"`
	ExpiresIn  int    `json:"expires_in"`
	Interval   int    `json:"interval"`
}
type DeviceResult struct {
	Status string `json:"status"`
	Token  string `json:"token,omitempty"`
}

func deviceCode(raw string) string {
	return strings.ToUpper(strings.Join(strings.Fields(strings.ReplaceAll(raw, "-", "")), ""))
}
func (s *Security) StartDevice(ctx context.Context, address, machine string) (DeviceStart, error) {
	out := DeviceStart{}
	if err := s.auth.throttle(ctx, "device-start:"+address, 10, 5*time.Minute); err != nil {
		return out, err
	}
	machine = strings.TrimSpace(machine)
	if len(machine) > 100 || strings.IndexFunc(machine, unicode.IsControl) >= 0 {
		return out, &accounts.FieldError{Field: "machine", Message: "Use a machine name of at most 100 characters."}
	}
	secret, err := Token()
	if err != nil {
		return out, err
	}
	code, err := newSetupCode()
	if err != nil {
		return out, err
	}
	description := "CLI · acta2"
	if machine != "" {
		description += " · " + machine
	}
	err = s.store.CreateDevice(ctx, DeviceRequest{Digest: Digest(secret), CodeDigest: Digest(code), Description: description, State: "pending", ExpiresAt: s.now().Add(DeviceLifetime)})
	if err != nil {
		return out, err
	}
	return DeviceStart{secret, code[:4] + "-" + code[4:], int(DeviceLifetime.Seconds()), 5}, nil
}
func (s *Security) DeviceInfo(ctx context.Context, code, address string) (DeviceRequest, error) {
	if err := s.auth.throttle(ctx, "device-code:"+address, 30, 5*time.Minute); err != nil {
		return DeviceRequest{}, err
	}
	d, err := s.store.ReadDevice(ctx, Digest(deviceCode(code)), true)
	if err != nil {
		return d, err
	}
	if !s.now().Before(d.ExpiresAt) || d.State != "pending" {
		return d, ErrDevice
	}
	return d, nil
}
func (s *Security) ApproveDevice(ctx context.Context, token, code, address string, approve bool, subject string) error {
	if strings.HasPrefix(token, "cli_") {
		return ErrForbidden
	}
	a, err := s.auth.Current(ctx, token)
	if err != nil {
		return err
	}
	d, err := s.DeviceInfo(ctx, code, address)
	if err != nil {
		return err
	}
	return s.store.WithDevice(ctx, a.ID, d.Digest, func(r *SecurityRecord, tx SecurityTx, d *DeviceRequest) error {
		if !s.now().Before(d.ExpiresAt) || d.State != "pending" {
			return ErrDevice
		}
		selected := subject
		if !approve {
			selected = ""
		}
		identity, parent, targetTx, err := s.connectionIdentity(ctx, r, tx, token, selected)
		if err != nil {
			return err
		}
		d.AccountID = identity.ID
		if !approve {
			d.State = "denied"
			return nil
		}
		secret, err := Token()
		if err != nil {
			return err
		}
		secret = "cli_" + secret
		d.Encrypted, err = seal(s.cipher, "device:"+string(d.Digest), secret)
		if err != nil {
			return err
		}
		now := s.now()
		session := SecuritySession{ID: uuid.NewString(), Digest: Digest(secret), AccountID: identity.ID, AuthorizedBy: a.ID, Description: d.Description, Kind: "cli", CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(SessionLifetime), Proof: parent.Proof}
		if err = targetTx.PutSession(ctx, session); err != nil {
			return err
		}
		d.State = "approved"
		return targetTx.Event(ctx, "cli_login", now)
	})
}
func (s *Security) PollDevice(ctx context.Context, secret string) (DeviceResult, error) {
	out := DeviceResult{}
	if !validToken(secret) || strings.HasPrefix(secret, "cli_") {
		return out, ErrDevice
	}
	d, err := s.store.ReadDevice(ctx, Digest(secret), false)
	if err != nil {
		return out, err
	}
	if err := s.auth.throttle(ctx, "device-poll:"+secret, 1, 5*time.Second); err != nil {
		return out, err
	}
	if !s.now().Before(d.ExpiresAt) || d.State == "consumed" {
		return out, ErrDevice
	}
	if d.State != "approved" {
		return DeviceResult{Status: d.State}, nil
	}
	err = s.store.WithDevice(ctx, d.AccountID, d.Digest, func(r *SecurityRecord, tx SecurityTx, d *DeviceRequest) error {
		if d.State != "approved" || !s.now().Before(d.ExpiresAt) {
			return ErrDevice
		}
		if accounts.RequiresMFASetup(r.Account) {
			return ErrMFARequired
		}
		if err := unseal(s.cipher, "device:"+string(d.Digest), d.Encrypted, &out.Token); err != nil {
			return err
		}
		if _, err := tx.Session(ctx, Digest(out.Token), s.now()); err != nil {
			return err
		}
		out.Status = "approved"
		d.State = "consumed"
		d.Encrypted = nil
		return nil
	})
	return out, err
}
