package codehosts

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/google/uuid"
)

var ErrDisconnected = errors.New("The host is disconnected. Start it or wait for it to reconnect.")
var ErrBusy = errors.New("The host has too many pending requests. Try again shortly.")

// Connections routes only within an authenticated owner. Pending operations
// belong to a specific connection and are never replayed after a disconnect.
type Connections struct {
	mu    sync.Mutex
	peers map[[2]string]*Peer
}

func NewConnections() *Connections { return &Connections{peers: make(map[[2]string]*Peer)} }
func (c *Connections) Attach(owner, id string, p *Peer) func() {
	key := [2]string{owner, id}
	c.mu.Lock()
	old := c.peers[key]
	c.peers[key] = p
	c.mu.Unlock()
	if old != nil {
		old.Close()
	}
	return func() {
		c.mu.Lock()
		if c.peers[key] == p {
			delete(c.peers, key)
		}
		c.mu.Unlock()
		p.Close()
	}
}
func (c *Connections) Get(owner, id string) *Peer {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.peers[[2]string{owner, id}]
}

type Peer struct {
	conn      *websocket.Conn
	authorize func(context.Context) error
	mu        sync.Mutex
	pending   map[string]chan Response
	providers *ProviderReport
	done      chan struct{}
	once      sync.Once
}

func NewPeer(conn *websocket.Conn, authorize func(context.Context) error) *Peer {
	return &Peer{conn: conn, authorize: authorize, pending: make(map[string]chan Response), done: make(chan struct{})}
}
func (p *Peer) Close() { p.once.Do(func() { close(p.done); _ = p.conn.CloseNow() }) }
func (p *Peer) Read(ctx context.Context) error {
	defer p.Close()
	for {
		var response Response
		if err := wsjson.Read(ctx, p.conn, &response); err != nil {
			return err
		}
		if response.ProviderStatus != nil {
			if response.ID != "" || response.Error != nil || len(response.Result) != 0 || !response.ProviderStatus.Valid() {
				return errors.New("invalid provider status notification")
			}
			response.ProviderStatus.CheckedAt = time.Now().UTC()
			p.mu.Lock()
			p.providers = response.ProviderStatus
			p.mu.Unlock()
			continue
		}
		p.mu.Lock()
		reply := p.pending[response.ID]
		p.mu.Unlock()
		if reply != nil {
			select {
			case reply <- response:
			default:
			}
		}
	}
}

// Status is connection-local and expires if the host stops reporting. The
// receipt timestamp comes from Acta, never the host's wall clock.
func (p *Peer) ProviderStatus(now time.Time) *ProviderReport {
	p.mu.Lock()
	defer p.mu.Unlock()
	select {
	case <-p.done:
		return nil
	default:
	}
	if p.providers == nil || now.Sub(p.providers.CheckedAt) > 90*time.Second {
		return nil
	}
	r := *p.providers
	r.Providers = append([]ProviderStatus(nil), r.Providers...)
	return &r
}
func (p *Peer) Call(ctx context.Context, request Request) (Response, error) {
	if err := p.authorize(ctx); err != nil {
		if ctx.Err() != nil {
			return Response{}, ctx.Err()
		}
		p.Close()
		return Response{}, ErrDisconnected
	}
	request.ID = uuid.NewString()
	reply := make(chan Response, 1)
	p.mu.Lock()
	if len(p.pending) >= 32 {
		p.mu.Unlock()
		return Response{}, ErrBusy
	}
	p.pending[request.ID] = reply
	p.mu.Unlock()
	defer func() { p.mu.Lock(); delete(p.pending, request.ID); p.mu.Unlock() }()
	select {
	case <-p.done:
		return Response{}, ErrDisconnected
	default:
	}
	write, cancel := context.WithTimeout(ctx, 5*time.Second)
	err := wsjson.Write(write, p.conn, request)
	cancel()
	if err != nil {
		p.Close()
		return Response{}, ErrDisconnected
	}
	select {
	case response := <-reply:
		if err := p.authorize(ctx); err != nil {
			if ctx.Err() != nil {
				return Response{}, ctx.Err()
			}
			p.Close()
			return Response{}, ErrDisconnected
		}
		return response, nil
	case <-p.done:
		return Response{}, ErrDisconnected
	case <-ctx.Done():
		return Response{}, ctx.Err()
	}
}
