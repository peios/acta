package threads

import (
	"context"
	"errors"
)

var ErrNotFound = errors.New("thread not found")

type Store interface {
	DeleteThread(context.Context, string, string) error
	ThreadControl(context.Context, string, string, string) (CommandStatus, error)
	RequestThreadControl(context.Context, string, Control) error
	PendingThreadControls(context.Context, string, string) ([]Control, error)
	CompleteThreadControl(context.Context, string, Result) error
	DiscoverThreads(context.Context, string, []Descriptor) error
	ListThreads(context.Context, string) ([]Descriptor, error)
	Thread(context.Context, string, string) (Descriptor, error)
	AppendThreadFrames(context.Context, string, string, []Frame) (int64, error)
	ThreadFrames(context.Context, string, string, int64) ([]Frame, error)
}
