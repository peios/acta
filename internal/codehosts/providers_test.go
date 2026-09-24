package codehosts

import (
	"testing"
	"time"
)

func TestProviderReportValidationAndExpiry(t *testing.T) {
	now := time.Now()
	report := ProviderReport{CheckedAt: now, Providers: []ProviderStatus{
		{ID: "claude", Installed: true, Auth: "signed_in"}, {ID: "codex", Auth: "unknown"},
	}}
	if !report.Valid() {
		t.Fatal("valid report rejected")
	}
	p := &Peer{providers: &report, done: make(chan struct{})}
	copy := p.ProviderStatus(now)
	copy.Providers[0].Auth = "changed"
	if p.ProviderStatus(now).Providers[0].Auth != "signed_in" {
		t.Fatal("report not copied")
	}
	if p.ProviderStatus(now.Add(91*time.Second)) != nil {
		t.Fatal("stale report retained")
	}
	close(p.done)
	if p.ProviderStatus(now) != nil {
		t.Fatal("closed connection retained report")
	}
	for _, bad := range []ProviderReport{
		{},
		{Providers: []ProviderStatus{{ID: "claude", Auth: "unknown"}, {ID: "claude", Auth: "unknown"}}},
		{Providers: []ProviderStatus{{ID: "claude", Auth: "signed_in"}, {ID: "codex", Auth: "unknown"}}},
		{Providers: []ProviderStatus{{ID: "claude", Auth: "unknown"}, {ID: "unknown", Auth: "unknown"}}},
	} {
		if bad.Valid() {
			t.Fatal("invalid report accepted", bad)
		}
	}
}
