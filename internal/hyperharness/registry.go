// Package hyperharness describes live connections, not persistent machines.
package hyperharness

import (
	"slices"
	"sort"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"acta2/internal/providers"
	"acta2/internal/threads"

	"github.com/google/uuid"
)

const Protocol = "acta-harness-v7"
const Heartbeat = 15 * time.Second
const ResponseTimeout = 5 * time.Second

// Connection IDs last only for the lifetime of a transport connection.
type Connection struct {
	ID          string               `json:"id"`
	Hostname    string               `json:"hostname"`
	ConnectedAt time.Time            `json:"connected_at"`
	Results     []threads.Result     `json:"results"`
	Threads     []threads.Descriptor `json:"threads"`
	Providers   []providers.Status   `json:"providers"`
}
type Snapshot struct {
	Type        string       `json:"type"`
	Connections []Connection `json:"connections"`
}
type Hello struct {
	Hostname string `json:"hostname"`
}

func ValidHostname(name string) bool {
	if name == "" || len(name) > 253 || !utf8.ValidString(name) {
		return false
	}
	for _, r := range name {
		if unicode.IsSpace(r) || unicode.IsControl(r) || !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}

type ownerState struct {
	connections map[string]Connection
	watchers    map[chan struct{}]struct{}
}
type Registry struct {
	mu       sync.Mutex
	owners   map[string]*ownerState
	commands map[string]chan threads.Control
}

func NewRegistry() *Registry {
	return &Registry{owners: make(map[string]*ownerState), commands: map[string]chan threads.Control{}}
}
func (r *Registry) owner(id string) *ownerState {
	s := r.owners[id]
	if s == nil {
		s = &ownerState{connections: make(map[string]Connection), watchers: make(map[chan struct{}]struct{})}
		r.owners[id] = s
	}
	return s
}
func (r *Registry) prune(owner string, s *ownerState) {
	if len(s.connections) == 0 && len(s.watchers) == 0 {
		delete(r.owners, owner)
	}
}
func notify(s *ownerState) {
	for ch := range s.watchers {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}
func (r *Registry) Connect(owner, hostname string) (Connection, func()) {
	r.mu.Lock()
	c := Connection{ID: uuid.NewString(), Hostname: hostname, ConnectedAt: time.Now().UTC(), Providers: providers.Initial()}
	s := r.owner(owner)
	s.connections[c.ID] = c
	r.commands[c.ID] = make(chan threads.Control, 32)
	notify(s)
	r.mu.Unlock()
	var once sync.Once
	c.Providers = slices.Clone(c.Providers)
	return c, func() {
		once.Do(func() {
			r.mu.Lock()
			defer r.mu.Unlock()
			delete(s.connections, c.ID)
			delete(r.commands, c.ID)
			notify(s)
			r.prune(owner, s)
		})
	}
}
func (r *Registry) Snapshot(owner string) Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := Snapshot{Type: "snapshot", Connections: []Connection{}}
	if s := r.owners[owner]; s != nil {
		for _, c := range s.connections {
			c.Providers = slices.Clone(c.Providers)
			c.Threads = slices.Clone(c.Threads)
			c.Results = slices.Clone(c.Results)
			result.Connections = append(result.Connections, c)
		}
	}
	sort.Slice(result.Connections, func(i, j int) bool {
		a, b := result.Connections[i], result.Connections[j]
		if a.Hostname != b.Hostname {
			return a.Hostname < b.Hostname
		}
		if !a.ConnectedAt.Equal(b.ConnectedAt) {
			return a.ConnectedAt.Before(b.ConnectedAt)
		}
		return a.ID < b.ID
	})
	return result
}

// Subscribe before reading the snapshot to avoid a gap between initial state
// and changes. Notifications coalesce; callers always read the latest snapshot.
func (r *Registry) Subscribe(owner string) (<-chan struct{}, func()) {
	r.mu.Lock()
	s := r.owner(owner)
	ch := make(chan struct{}, 1)
	s.watchers[ch] = struct{}{}
	r.mu.Unlock()
	var once sync.Once
	return ch, func() {
		once.Do(func() { r.mu.Lock(); defer r.mu.Unlock(); delete(s.watchers, ch); r.prune(owner, s) })
	}
}

// UpdateProviders never creates or revives a connection. Readers receive copies.
func (r *Registry) UpdateProviders(owner, id string, states []providers.Status) bool {
	if !providers.ValidSnapshot(states) {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.owners[owner]
	if s == nil {
		return false
	}
	c, ok := s.connections[id]
	if !ok {
		return false
	}
	if slices.Equal(c.Providers, states) {
		return true
	}
	c.Providers = slices.Clone(states)
	s.connections[id] = c
	notify(s)
	return true
}

// ProviderUpdate is the only client application message after the hello in v2.
type ProviderUpdate struct {
	Threads   []threads.Descriptor `json:"threads,omitempty"`
	ThreadID  string               `json:"thread_id,omitempty"`
	Frames    []threads.Frame      `json:"frames,omitempty"`
	Result    *threads.Result      `json:"result,omitempty"`
	Type      string               `json:"type"`
	Providers []providers.Status   `json:"providers"`
}

func (r *Registry) Commands(id string) <-chan threads.Control {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.commands[id]
}
func (r *Registry) Send(owner, connection string, q threads.Control) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.owners[owner]
	if s == nil {
		return false
	}
	if _, ok := s.connections[connection]; !ok {
		return false
	}
	select {
	case r.commands[connection] <- q:
		return true
	default:
		return false
	}
}
func (r *Registry) Claim(owner, connection string, list []threads.Descriptor) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.owners[owner]
	if s == nil {
		return false
	}
	current, ok := s.connections[connection]
	if !ok {
		return false
	}
	for _, t := range list {
		for id, c := range s.connections {
			if id != connection {
				for _, other := range c.Threads {
					if other.ID == t.ID {
						return false
					}
				}
			}
		}
	}
	current.Threads = slices.Clone(list)
	s.connections[connection] = current
	notify(s)
	return true
}
func (r *Registry) ThreadConnection(owner, id string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s := r.owners[owner]; s != nil {
		for connection, c := range s.connections {
			for _, t := range c.Threads {
				if t.ID == id {
					return connection
				}
			}
		}
	}
	return ""
}

func (r *Registry) RecordResult(owner, connection string, result threads.Result) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.owners[owner]
	if s == nil {
		return
	}
	c, ok := s.connections[connection]
	if !ok {
		return
	}
	for _, old := range c.Results {
		if old.ID == result.ID {
			return
		}
	}
	c.Results = append(c.Results, result)
	if len(c.Results) > 32 {
		c.Results = c.Results[len(c.Results)-32:]
	}
	s.connections[connection] = c
	notify(s)
}
