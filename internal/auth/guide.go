package auth

import (
	"acta2/internal/accounts"
	"acta2/internal/guide"
	"context"
)

type GuideTx interface {
	GuidePreference(context.Context, string) (guide.Preference, error)
	SaveGuidePreference(context.Context, string, guide.Save) (guide.Preference, error)
}

func guidePreferences(ctx context.Context, tx WorkspaceTx) (guide.Preferences, error) {
	a := tx.Actor()
	owner := a.ID
	if a.IsAgent() {
		owner = *a.ParentID
	}
	var out guide.Preferences
	var err error
	out.Site, err = tx.GuidePreference(ctx, "site")
	if err != nil {
		return out, err
	}
	out.User, err = tx.GuidePreference(ctx, owner)
	out.Site.CanWrite = !a.IsAgent() && accounts.CheckPermission(a, accounts.WriteSiteGuide)
	out.User.CanWrite = !a.IsAgent()
	return out, err
}
func (m *Management) GuidePreferences(ctx context.Context, token string) (guide.Preferences, error) {
	var out guide.Preferences
	err := m.workspaceTx(ctx, token, false, func(tx WorkspaceTx) error {
		var err error
		out, err = guidePreferences(ctx, tx)
		return err
	})
	return out, err
}
func (m *Management) SaveGuidePreferences(ctx context.Context, token string, in guide.Save) (guide.Preference, error) {
	var out guide.Preference
	if err := guide.Validate(in); err != nil {
		return out, err
	}
	err := m.workspaceTx(ctx, token, true, func(tx WorkspaceTx) error {
		a := tx.Actor()
		// Policy is authored by people. Agent grants never allow policy edits.
		if a.IsAgent() {
			return ErrForbidden
		}
		key := a.ID
		if in.Scope == "site" {
			if !accounts.CheckPermission(a, accounts.WriteSiteGuide) {
				return ErrForbidden
			}
			key = "site"
		}
		var err error
		out, err = tx.SaveGuidePreference(ctx, key, in)
		out.CanWrite = true
		return err
	})
	return out, err
}
