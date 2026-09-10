// Package providers contains local provider adapters and their public discovery
// contract. Provider processes and credentials are never owned by the server.
package providers

import "regexp"

type Status struct {
	ID             string `json:"id"`
	Installation   string `json:"installation"`
	Version        string `json:"version,omitempty"`
	Authentication string `json:"authentication"`
	Issue          string `json:"issue,omitempty"`
}

var versionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:[-+][A-Za-z0-9.+-]+)?$`)

func validVersion(v string) bool { return len(v) <= 64 && versionPattern.MatchString(v) }
func Initial() []Status {
	return []Status{{ID: "codex", Installation: "checking", Authentication: "unknown"}, {ID: "claude", Installation: "checking", Authentication: "unknown"}}
}

// ValidSnapshot constrains untrusted client advertisements to the public schema.
// IDs name adapters, not machines or provider accounts.
func ValidSnapshot(states []Status) bool {
	if len(states) != 2 {
		return false
	}
	seen := map[string]bool{}
	for _, s := range states {
		if (s.ID != "codex" && s.ID != "claude") || seen[s.ID] {
			return false
		}
		seen[s.ID] = true
		switch s.Installation {
		case "checking", "installed", "not_installed", "unknown":
		default:
			return false
		}
		switch s.Authentication {
		case "unknown", "signed_in", "signed_out":
		default:
			return false
		}
		switch s.Issue {
		case "", "timeout", "probe_failed", "version_unknown", "auth_unavailable":
		default:
			return false
		}
		if s.Version != "" && !validVersion(s.Version) {
			return false
		}
		if s.Installation != "installed" && (s.Version != "" || s.Authentication != "unknown") {
			return false
		}
	}
	return true
}
