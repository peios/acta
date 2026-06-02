package local

import (
	"sync"
	"time"
)

// ThrottleConfig tunes the login brute-force guard. The zero value disables
// everything (IPMax <= 0 means no IP blocking; BackoffStep <= 0 means no
// backoff), which is how tests get an inert guard.
type ThrottleConfig struct {
	// Window is how long a failed attempt is remembered, for both the per-IP
	// counter and the per-username backoff.
	Window time.Duration
	// IPMax is the number of failures from a single client IP within Window that
	// triggers blocking. Further attempts are refused without verifying a
	// password (so they cost no argon2 work) until older failures age out.
	IPMax int
	// BackoffStep is the delay added per consecutive failure against a username;
	// BackoffMax caps it. This soft slow-down never hard-locks an account.
	BackoffStep time.Duration
	BackoffMax  time.Duration
}

// Guard is an in-memory login brute-force limiter: a per-IP sliding-window
// failure count plus a per-username response backoff. It is safe for concurrent
// use. A nil *Guard is valid and inert, so callers needn't branch on its
// presence.
type Guard struct {
	cfg ThrottleConfig
	mu  sync.Mutex
	// ipFails and userFails hold the timestamps of recent failures, pruned to
	// Window on access.
	ipFails   map[string][]time.Time
	userFails map[string][]time.Time

	now   func() time.Time    // injectable for tests
	sleep func(time.Duration) // injectable for tests
}

// NewGuard builds a guard with the given policy. Wire it into a Provider with
// WithThrottle.
func NewGuard(cfg ThrottleConfig) *Guard {
	return &Guard{
		cfg:       cfg,
		ipFails:   make(map[string][]time.Time),
		userFails: make(map[string][]time.Time),
		now:       time.Now,
		sleep:     time.Sleep,
	}
}

// Allowed reports whether ip may attempt a password login right now. A blocked
// IP should be refused before any password is verified.
func (g *Guard) Allowed(ip string) bool {
	if g == nil || g.cfg.IPMax <= 0 || ip == "" {
		return true
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	live := g.pruned(g.ipFails[ip])
	if len(live) == 0 {
		delete(g.ipFails, ip)
	} else {
		g.ipFails[ip] = live
	}
	return len(live) < g.cfg.IPMax
}

// RecordFailure registers a failed attempt from ip against username and returns
// the backoff delay the caller should apply before responding. username may be
// empty (e.g. a malformed submission), in which case only the IP is counted.
func (g *Guard) RecordFailure(ip, username string) time.Duration {
	if g == nil {
		return 0
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.now()

	if g.cfg.IPMax > 0 && ip != "" {
		g.ipFails[ip] = append(g.pruned(g.ipFails[ip]), now)
	}

	if g.cfg.BackoffStep <= 0 || username == "" {
		return 0
	}
	fails := append(g.pruned(g.userFails[username]), now)
	g.userFails[username] = fails
	delay := time.Duration(len(fails)) * g.cfg.BackoffStep
	if g.cfg.BackoffMax > 0 && delay > g.cfg.BackoffMax {
		delay = g.cfg.BackoffMax
	}
	return delay
}

// RecordSuccess clears a username's accumulated backoff after a correct login.
func (g *Guard) RecordSuccess(username string) {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.userFails, username)
}

// Backoff blocks for the given delay using the guard's (injectable) sleeper. A
// zero or negative delay returns immediately.
func (g *Guard) Backoff(d time.Duration) {
	if g == nil || d <= 0 {
		return
	}
	g.sleep(d)
}

// Sweep evicts fully-aged-out entries so the maps don't grow without bound. The
// server runs it periodically; it's a no-op on a nil or disabled guard.
func (g *Guard) Sweep() {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for ip, ts := range g.ipFails {
		if live := g.pruned(ts); len(live) == 0 {
			delete(g.ipFails, ip)
		} else {
			g.ipFails[ip] = live
		}
	}
	for u, ts := range g.userFails {
		if live := g.pruned(ts); len(live) == 0 {
			delete(g.userFails, u)
		} else {
			g.userFails[u] = live
		}
	}
}

// pruned returns the timestamps still within Window. The caller holds the lock.
func (g *Guard) pruned(ts []time.Time) []time.Time {
	cutoff := g.now().Add(-g.cfg.Window)
	out := ts[:0:0] // fresh backing array; never alias the caller's slice
	for _, t := range ts {
		if t.After(cutoff) {
			out = append(out, t)
		}
	}
	return out
}
