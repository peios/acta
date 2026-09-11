package auth

import (
	"acta/internal/tasks"
	ws "acta/internal/workspaces"
	"context"
)

func (m *Management) TaskFollowing(ctx context.Context, token, ref string) (bool, error) {
	var following bool
	err := m.commentScope(ctx, token, ref, false, func(tx WorkspaceTx, t tasks.Task, _ ws.Access) error {
		if tx.Actor().IsAgent() {
			return ErrForbidden
		}
		var err error
		following, err = tx.TaskFollowing(ctx, t.ID)
		return err
	})
	return following, err
}
func (m *Management) SetTaskFollowing(ctx context.Context, token, ref string, following bool) error {
	return m.commentScope(ctx, token, ref, true, func(tx WorkspaceTx, t tasks.Task, _ ws.Access) error {
		if tx.Actor().IsAgent() {
			return ErrForbidden
		}
		return tx.SetTaskFollowing(ctx, t.ID, following)
	})
}
