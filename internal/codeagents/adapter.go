// Package codeagents translates provider processes into Acta Code Thread output.
// It has no server, browser or codebase catalogue dependencies.
package codeagents

import (
	"acta/internal/codethreads"
	"context"
)

type Launch struct{ Directory string }

// Start returns after the provider startup handshake succeeds, or fails within
// a bounded timeout. Emission begins immediately, including before Start returns,
// and ends before Wait returns. A failed Start cleans up its process and readers.
// Context owns the process lifetime, independently of the startup deadline.
type Adapter interface {
	Start(context.Context, Launch, func(codethreads.Payload)) (Session, error)
	// NewConverter returns independent, side-effect-free conversion state. It
	// must not depend on a live Session or perform process, network or file I/O.
	NewConverter() Converter
}
type Session interface {
	Wait() error
	// Send starts one turn and waits for its terminal event. User acknowledgement
	// is emitted separately as message.user. A nil error does not imply the model
	// task succeeded; task errors remain in the provider's raw output.
	Send(context.Context, codethreads.Input) error
}
