package auth

import (
	"context"
	"errors"

	"acta/internal/notifications"
)

func (s *Service) Notifications(ctx context.Context, owner string) (notifications.NotificationPage, error) {
	store, ok := s.store.(notifications.NotificationStore)
	if !ok {
		return notifications.NotificationPage{}, errors.New("notification storage is unavailable")
	}
	return store.Notifications(ctx, owner)
}
func (s *Service) ReadNotifications(ctx context.Context, owner string, reads []notifications.NotificationRead) error {
	store, ok := s.store.(notifications.NotificationStore)
	if !ok {
		return errors.New("notification storage is unavailable")
	}
	return store.ReadNotifications(ctx, owner, reads)
}
