package hyperharness

import (
	"acta/internal/harnesspipe"
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"os"
	"path/filepath"
	"time"
)

// Native history is read locally. A filename alone is not proof of resumability:
// require a complete stored user record belonging to this exact session.
func claudeSavedHistory(root, native string) bool {
	if _, err := uuid.Parse(native); err != nil {
		return false
	}
	paths, err := filepath.Glob(filepath.Join(root, "projects", "*", native+".jsonl"))
	if err != nil {
		return false
	}
	for _, path := range paths {
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 4096), 2<<20)
		found := false
		for n := 0; n < 10000 && scanner.Scan(); n++ {
			var record struct {
				Type    string `json:"type"`
				Session string `json:"sessionId"`
				Message struct {
					Role    string          `json:"role"`
					Content json.RawMessage `json:"content"`
				} `json:"message"`
			}
			if json.Unmarshal(scanner.Bytes(), &record) == nil && record.Type == "user" && record.Session == native && record.Message.Role == "user" && len(record.Message.Content) > 2 && string(record.Message.Content) != "null" {
				found = true
				break
			}
		}
		_ = f.Close()
		if found {
			return true
		}
	}
	return false
}
func claudeConfigRoot() string {
	if root := os.Getenv("CLAUDE_CONFIG_DIR"); root != "" {
		return root
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude")
}

// Refresh cheap provider observations after startup and completed turns. The
// native summary is an estimate and performs no token-count model request.
// Poll MCP while starting; the rest is refreshed at most once every 30 seconds.
func (c *Controller) observeClaude(id string) {
	var after int64
	var lastRefresh time.Time
	pendingMCP := false
	for {
		select {
		case <-c.ctx.Done():
			return
		default:
		}
		c.mu.Lock()
		r := c.records[id]
		if r == nil || r.Thread.State != "running" {
			c.mu.Unlock()
			return
		}
		snapshot := *r
		c.mu.Unlock()
		if snapshot.Action == "" {
			if !snapshot.Thread.Committed && claudeSavedHistory(claudeConfigRoot(), snapshot.Thread.ProviderID) {
				c.mu.Lock()
				current := c.records[id]
				if current.Thread.RunID == snapshot.Thread.RunID && !current.Thread.Committed {
					updated := *current
					updated.Thread.Committed = true
					updated.Thread.Revision++
					if c.save(&updated) == nil {
						c.records[id] = &updated
					}
				}
				c.mu.Unlock()
			}
			ctx, cancel := context.WithTimeout(c.ctx, 15*time.Second)
			captures, err := c.pipe.Read(ctx, harnesspipe.ReadRequest{ThreadID: id, After: after})
			refresh := lastRefresh.IsZero() || time.Since(lastRefresh) > 30*time.Second
			if err == nil {
				for _, f := range captures {
					after = f.Sequence
					if f.RunID != snapshot.Thread.RunID {
						continue
					}
					var m struct {
						Type string `json:"type"`
					}
					if json.Unmarshal(f.Data, &m) == nil && m.Type == "result" {
						refresh = true
					}
				}
			}
			if refresh || pendingMCP {
				c.mu.Lock()
				current := c.records[id]
				ready := current.Action == "" && current.Thread.RunID == snapshot.Thread.RunID
				if ready {
					snapshot = *current
					snapshot.ProbeAttempt++
					if c.save(&snapshot) != nil {
						ready = false
					} else {
						c.records[id] = &snapshot
					}
				}
				c.mu.Unlock()
				if ready {
					body, e := c.rpcID(ctx, &snapshot, "mcp_status", fmt.Sprintf("mcp_status/observe/%d", snapshot.ProbeAttempt), map[string]any{})
					if e == nil {
						var reply struct {
							Servers []struct {
								Status string `json:"status"`
							} `json:"mcpServers"`
						}
						if json.Unmarshal(body, &reply) == nil {
							pendingMCP = false
							for _, server := range reply.Servers {
								pendingMCP = pendingMCP || server.Status == "pending"
							}
						}
					}
					if refresh {
						for _, request := range []struct {
							name   string
							params map[string]any
						}{
							{"get_context_usage", map[string]any{"detail": "summary"}},
							{"get_usage", map[string]any{"skip_behaviors": true}},
						} {
							if _, err = c.rpcID(ctx, &snapshot, request.name, fmt.Sprintf("%s/observe/%d", request.name, snapshot.ProbeAttempt), request.params); err != nil {
								break
							}
						}
						lastRefresh = time.Now()
					}
				}
			}
			cancel()
		}
		select {
		case <-c.ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}
