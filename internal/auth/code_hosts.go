package auth

import (
	"context"
	"errors"
	"strings"

	"acta/internal/codehosts"
)

func (s *Service) codeHostOwner(ctx context.Context, token string, native bool) (string, codehosts.Store, error) {
	if native && !strings.HasPrefix(token, "cli_") {
		return "", nil, ErrForbidden
	}
	a, err := s.Current(ctx, token)
	if err != nil {
		return "", nil, err
	}
	// Hosts belong only to the authenticated human. Agent credentials and
	// superuser permissions never expand this boundary to another account.
	if a.ParentID != nil {
		return "", nil, ErrForbidden
	}
	store, ok := s.store.(codehosts.Store)
	if !ok {
		return "", nil, errors.New("code host storage unavailable")
	}
	return a.ID, store, nil
}

func (s *Service) CodeHosts(ctx context.Context, token string) ([]codehosts.Host, error) {
	owner, store, err := s.codeHostOwner(ctx, token, false)
	if err != nil {
		return nil, err
	}
	return store.ListCodeHosts(ctx, owner, s.now())
}

// CodeHostOwner authenticates the exact human used to route a host connection.
func (s *Service) CodeHostOwner(ctx context.Context, token string, native bool) (string, error) {
	owner, _, err := s.codeHostOwner(ctx, token, native)
	return owner, err
}

func (s *Service) RenewCodeHost(ctx context.Context, token string, in codehosts.Heartbeat) (codehosts.Host, error) {
	owner, store, err := s.codeHostOwner(ctx, token, true)
	if err != nil {
		return codehosts.Host{}, err
	}
	if err = in.Validate(); err != nil {
		return codehosts.Host{}, err
	}
	return store.RenewCodeHost(ctx, owner, Digest(token), in, s.now())
}

func (s *Service) ReleaseCodeHost(ctx context.Context, token, id, instance string) error {
	owner, store, err := s.codeHostOwner(ctx, token, true)
	if err != nil {
		return err
	}
	if err = codehosts.ValidateIDs(id, instance); err != nil {
		return err
	}
	return store.ReleaseCodeHost(ctx, owner, Digest(token), id, instance)
}
