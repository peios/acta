package codehosts

import "time"

// ProviderStatus contains only safe discovery facts, never CLI output or credentials.
type ProviderStatus struct {
	ID        string `json:"id"`
	Installed bool   `json:"installed"`
	Auth      string `json:"auth"`
}

type ProviderReport struct {
	Providers []ProviderStatus `json:"providers"`
	CheckedAt time.Time        `json:"checked_at"`
}

func (r ProviderReport) Valid() bool {
	if len(r.Providers) != 2 {
		return false
	}
	seen := make(map[string]bool, 2)
	for _, p := range r.Providers {
		if (p.ID != "claude" && p.ID != "codex") || seen[p.ID] {
			return false
		}
		seen[p.ID] = true
		if p.Auth != "signed_in" && p.Auth != "signed_out" && p.Auth != "unknown" {
			return false
		}
		if !p.Installed && p.Auth != "unknown" {
			return false
		}
	}
	return true
}
