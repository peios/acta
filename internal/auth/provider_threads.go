package auth

import (
	"acta/internal/conversation"
	"acta/internal/threads"
	"context"
	"errors"
)

func (s *Service) threadStore() (threads.Store, error) {
	store, ok := s.store.(threads.Store)
	if !ok {
		return nil, errors.New("thread storage is unavailable")
	}
	return store, nil
}
func (s *Service) DiscoverThreads(ctx context.Context, owner string, list []threads.Descriptor) error {
	store, err := s.threadStore()
	if err != nil {
		return err
	}
	return store.DiscoverThreads(ctx, owner, list)
}
func (s *Service) ListThreads(ctx context.Context, owner string) ([]threads.Descriptor, error) {
	store, err := s.threadStore()
	if err != nil {
		return nil, err
	}
	return store.ListThreads(ctx, owner)
}
func (s *Service) Thread(ctx context.Context, owner, id string) (threads.Descriptor, error) {
	store, err := s.threadStore()
	if err != nil {
		return threads.Descriptor{}, err
	}
	return store.Thread(ctx, owner, id)
}
func (s *Service) AppendThreadFrames(ctx context.Context, owner, id string, frames []threads.Frame) (int64, error) {
	store, err := s.threadStore()
	if err != nil {
		return 0, err
	}
	return store.AppendThreadFrames(ctx, owner, id, frames)
}
func (s *Service) ThreadFrames(ctx context.Context, owner, id string, after int64) ([]threads.Frame, error) {
	store, err := s.threadStore()
	if err != nil {
		return nil, err
	}
	return store.ThreadFrames(ctx, owner, id, after)
}

func (s *Service) RequestThreadControl(ctx context.Context, owner string, q threads.Control) error {
	store, err := s.threadStore()
	if err != nil {
		return err
	}
	return store.RequestThreadControl(ctx, owner, q)
}
func (s *Service) PendingThreadControls(ctx context.Context, owner, id string) ([]threads.Control, error) {
	store, err := s.threadStore()
	if err != nil {
		return nil, err
	}
	return store.PendingThreadControls(ctx, owner, id)
}
func (s *Service) CompleteThreadControl(ctx context.Context, owner string, result threads.Result) error {
	store, err := s.threadStore()
	if err != nil {
		return err
	}
	return store.CompleteThreadControl(ctx, owner, result)
}

func (s *Service) ThreadControl(ctx context.Context, owner, id, command string) (threads.CommandStatus, error) {
	store, err := s.threadStore()
	if err != nil {
		return threads.CommandStatus{}, err
	}
	return store.ThreadControl(ctx, owner, id, command)
}

func (s *Service) Conversation(ctx context.Context, owner, id string, q conversation.Query) (conversation.Page, error) {
	store, ok := s.store.(interface {
		Conversation(context.Context, string, string, conversation.Query) (conversation.Page, error)
	})
	if !ok {
		return conversation.Page{}, errors.New("conversation storage is unavailable")
	}
	return store.Conversation(ctx, owner, id, q)
}

func (s *Service) DeleteThread(ctx context.Context, owner, id string) error {
	store, err := s.threadStore()
	if err != nil {
		return err
	}
	return store.DeleteThread(ctx, owner, id)
}

func (s *Service) ThreadNotifications(ctx context.Context, owner string) (threads.NotificationPage, error) {
	store, ok := s.store.(threads.NotificationStore)
	if !ok {
		return threads.NotificationPage{}, errors.New("notification storage is unavailable")
	}
	return store.ThreadNotifications(ctx, owner)
}
func (s *Service) ReadThreadNotifications(ctx context.Context, owner string, reads []threads.NotificationRead) error {
	store, ok := s.store.(threads.NotificationStore)
	if !ok {
		return errors.New("notification storage is unavailable")
	}
	return store.ReadThreadNotifications(ctx, owner, reads)
}
