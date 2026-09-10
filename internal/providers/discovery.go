package providers

import (
	"context"
	"slices"
	"sync"
	"time"
)

type Source interface {
	Snapshot() []Status
	Subscribe() (<-chan struct{}, func())
}

// Discovery outlives individual server connections. Each adapter has its own
// serial probe loop; a slow probe cannot block another adapter or a heartbeat.
type Discovery struct {
	mu       sync.Mutex
	states   []Status
	watchers map[chan struct{}]struct{}
}

func NewDiscovery() *Discovery {
	return &Discovery{states: Initial(), watchers: map[chan struct{}]struct{}{}}
}
func (d *Discovery) Snapshot() []Status {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.states)
}
func (d *Discovery) Subscribe() (<-chan struct{}, func()) {
	d.mu.Lock()
	ch := make(chan struct{}, 1)
	d.watchers[ch] = struct{}{}
	d.mu.Unlock()
	var once sync.Once
	return ch, func() { once.Do(func() { d.mu.Lock(); defer d.mu.Unlock(); delete(d.watchers, ch) }) }
}
func (d *Discovery) publish(s Status) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for i, current := range d.states {
		if current.ID == s.ID {
			if current == s {
				return
			}
			d.states[i] = s
			for ch := range d.watchers {
				select {
				case ch <- struct{}{}:
				default:
				}
			}
			return
		}
	}
}
func (d *Discovery) Run(ctx context.Context, adapters []Adapter) {
	d.run(ctx, adapters, RefreshInterval, ProbeTimeout)
}
func (d *Discovery) run(ctx context.Context, adapters []Adapter, interval, timeout time.Duration) {
	var wg sync.WaitGroup
	for _, adapter := range adapters {
		wg.Go(func() {
			for ctx.Err() == nil {
				probe, cancel := context.WithTimeout(ctx, timeout)
				s := adapter.Discover(probe)
				cancel()
				if ctx.Err() != nil {
					return
				}
				d.publish(s)
				timer := time.NewTimer(interval)
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
			}
		})
	}
	wg.Wait()
}
