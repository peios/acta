package local

import (
	"testing"
	"time"
)

func TestGuardIPBlocking(t *testing.T) {
	now := time.Unix(1000, 0)
	g := NewGuard(ThrottleConfig{Window: 15 * time.Minute, IPMax: 3})
	g.now = func() time.Time { return now }

	const ip = "203.0.113.5"
	for i := range 3 {
		if !g.Allowed(ip) {
			t.Fatalf("attempt %d blocked too early", i)
		}
		g.RecordFailure(ip, "alice")
	}
	if g.Allowed(ip) {
		t.Fatal("want IP blocked after reaching IPMax failures")
	}
	if !g.Allowed("198.51.100.9") {
		t.Fatal("an unrelated IP must not be affected")
	}
}

func TestGuardWindowExpiry(t *testing.T) {
	now := time.Unix(1000, 0)
	g := NewGuard(ThrottleConfig{Window: time.Minute, IPMax: 2})
	g.now = func() time.Time { return now }

	const ip = "203.0.113.5"
	g.RecordFailure(ip, "")
	g.RecordFailure(ip, "")
	if g.Allowed(ip) {
		t.Fatal("want blocked at IPMax")
	}
	now = now.Add(2 * time.Minute) // age the failures out of the window
	if !g.Allowed(ip) {
		t.Fatal("failures should have aged out, unblocking the IP")
	}
}

func TestGuardBackoffGrowsAndCaps(t *testing.T) {
	now := time.Unix(1000, 0)
	g := NewGuard(ThrottleConfig{Window: time.Hour, BackoffStep: time.Second, BackoffMax: 3 * time.Second})
	g.now = func() time.Time { return now }

	want := []time.Duration{time.Second, 2 * time.Second, 3 * time.Second, 3 * time.Second}
	for i, w := range want {
		// Empty IP isolates the per-username backoff path.
		if got := g.RecordFailure("", "bob"); got != w {
			t.Errorf("failure %d: backoff = %v, want %v", i+1, got, w)
		}
	}
}

func TestGuardSuccessResetsBackoff(t *testing.T) {
	now := time.Unix(1000, 0)
	g := NewGuard(ThrottleConfig{Window: time.Hour, BackoffStep: time.Second, BackoffMax: 10 * time.Second})
	g.now = func() time.Time { return now }

	g.RecordFailure("", "carol")
	g.RecordFailure("", "carol")
	g.RecordSuccess("carol")
	if got := g.RecordFailure("", "carol"); got != time.Second {
		t.Errorf("after success the backoff should restart at one step, got %v", got)
	}
}

func TestGuardBackoffSleeper(t *testing.T) {
	g := NewGuard(ThrottleConfig{Window: time.Hour, BackoffStep: time.Second})
	var slept time.Duration
	g.sleep = func(d time.Duration) { slept += d }

	g.Backoff(2 * time.Second)
	g.Backoff(0)  // no-op
	g.Backoff(-1) // no-op
	if slept != 2*time.Second {
		t.Errorf("slept = %v, want 2s", slept)
	}
}

func TestGuardSweepEvicts(t *testing.T) {
	now := time.Unix(1000, 0)
	g := NewGuard(ThrottleConfig{Window: time.Minute, IPMax: 5, BackoffStep: time.Second})
	g.now = func() time.Time { return now }

	g.RecordFailure("203.0.113.5", "dave")
	now = now.Add(2 * time.Minute)
	g.Sweep()

	g.mu.Lock()
	nip, nuser := len(g.ipFails), len(g.userFails)
	g.mu.Unlock()
	if nip != 0 || nuser != 0 {
		t.Errorf("sweep left ip=%d user=%d entries, want 0/0", nip, nuser)
	}
}

func TestNilGuardInert(t *testing.T) {
	var g *Guard
	if !g.Allowed("x") {
		t.Fatal("nil guard should allow")
	}
	if d := g.RecordFailure("x", "y"); d != 0 {
		t.Fatalf("nil RecordFailure = %v, want 0", d)
	}
	g.RecordSuccess("y") // must not panic
	g.Backoff(time.Second)
	g.Sweep()
}

func TestDisabledGuardAlwaysAllows(t *testing.T) {
	g := NewGuard(ThrottleConfig{}) // zero config disables every limit
	for range 100 {
		if !g.Allowed("ip") {
			t.Fatal("disabled guard should always allow")
		}
		if d := g.RecordFailure("ip", "user"); d != 0 {
			t.Fatalf("disabled backoff = %v, want 0", d)
		}
	}
}
