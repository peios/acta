package accounts

import (
	"context"
	"errors"
)

var (
	ErrNameUnavailable    = errors.New("username is in use or reserved")
	ErrProfileChanged     = errors.New("profile changed since it was loaded")
	ErrAccountUnavailable = errors.New("account is absent or disabled")
)

type FieldError struct{ Field, Message string }

func (e *FieldError) Error() string { return e.Message }

// ProfileStore must atomically update an active account only at the expected
// version, claim its new name, retain previous names for their owner, and record
// the change. Names share one namespace for both active and reserved claims;
// an owner may reclaim a name but no other account may claim it concurrently.
type ProfileStore interface {
	UpdateProfile(context.Context, string, int64, string, *string) (Account, error)
}

type ProfileService struct{ store ProfileStore }

func NewProfileService(store ProfileStore) *ProfileService { return &ProfileService{store: store} }

// accountID is the authenticated UUID, never an identifier supplied by a form.
func (s *ProfileService) Update(ctx context.Context, accountID string, version int64, rawUsername, rawDisplayName string) (Account, error) {
	username, displayName, err := ValidateProfile(rawUsername, rawDisplayName)
	if err != nil {
		return Account{}, err
	}
	if version < 1 {
		return Account{}, ErrProfileChanged
	}
	a, err := s.store.UpdateProfile(ctx, accountID, version, username, displayName)
	if errors.Is(err, ErrNameUnavailable) {
		return Account{}, &FieldError{"username", "That username is in use or reserved. Choose another."}
	}
	return a, err
}

// ValidateProfile is shared by account creation and both profile-edit surfaces.
func ValidateProfile(rawUsername, rawDisplayName string) (string, *string, error) {
	username, err := Username(rawUsername)
	if err != nil {
		return "", nil, &FieldError{"username", err.Error()}
	}
	displayName, err := DisplayName(rawDisplayName)
	if err != nil {
		return "", nil, &FieldError{"display_name", err.Error()}
	}
	return username, displayName, nil
}
