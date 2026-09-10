package auth

import (
	"acta2/internal/push"
	"acta2/internal/threads"
	"context"
	"errors"
)

func (s *Service) pushStore() (push.Store, error) {
	p, ok := s.store.(push.Store)
	if !ok {
		return nil, errors.New("push storage unavailable")
	}
	return p, nil
}
func (s *Service) SavePush(ctx context.Context, owner, token string, sub push.Subscription) (string, error) {
	p, e := s.pushStore()
	if e != nil {
		return "", e
	}
	return p.SavePushSubscription(ctx, owner, Digest(token), sub)
}
func (s *Service) DeletePush(ctx context.Context, owner, token, endpoint string) error {
	p, e := s.pushStore()
	if e != nil {
		return e
	}
	return p.DeletePushSubscription(ctx, owner, Digest(token), endpoint)
}
func (s *Service) PushNotice(ctx context.Context, owner, token, subscription, id string, revision int64) (threads.Notification, error) {
	p, e := s.pushStore()
	if e != nil {
		return threads.Notification{}, e
	}
	return p.PushNotice(ctx, owner, Digest(token), subscription, id, revision)
}
