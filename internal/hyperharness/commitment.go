package hyperharness

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// savedUserHistory accepts full stored history only. Metadata, a path, an empty
// turn, and a live item/completed notification do not establish resumability.
func savedUserHistory(raw json.RawMessage, native string) bool {
	var result struct {
		Thread struct {
			ID    string `json:"id"`
			Turns []struct {
				ItemsView string `json:"itemsView"`
				Items     []struct {
					Type    string            `json:"type"`
					Content []json.RawMessage `json:"content"`
				} `json:"items"`
			} `json:"turns"`
		} `json:"thread"`
	}
	if json.Unmarshal(raw, &result) != nil || result.Thread.ID != native {
		return false
	}
	for _, turn := range result.Thread.Turns {
		if turn.ItemsView != "full" {
			continue
		}
		for _, item := range turn.Items {
			if item.Type == "userMessage" && len(item.Content) > 0 {
				return true
			}
		}
	}
	return false
}
func (c *Controller) probeCommitment(id string) {
	delay := 250 * time.Millisecond
	for {
		c.mu.Lock()
		r := c.records[id]
		if c.ctx.Err() != nil || r == nil || r.Thread.Committed || r.Thread.State != "running" || r.Action != "" {
			c.mu.Unlock()
			return
		}
		copy := *r
		copy.ProbeAttempt++
		if c.save(&copy) != nil {
			c.mu.Unlock()
			return
		}
		c.records[id] = &copy
		c.mu.Unlock()
		ctx, cancel := context.WithTimeout(c.ctx, 10*time.Second)
		result, err := c.rpcID(ctx, &copy, "thread/read", fmt.Sprintf("thread/read/%d", copy.ProbeAttempt), map[string]any{"threadId": copy.Thread.ProviderID, "includeTurns": true})
		cancel()
		if err == nil && savedUserHistory(result, copy.Thread.ProviderID) {
			c.mu.Lock()
			current := c.records[id]
			if current.Thread.RunID == copy.Thread.RunID && !current.Thread.Committed {
				updated := *current
				updated.Thread.Committed = true
				updated.Thread.Revision++
				if c.save(&updated) == nil {
					c.records[id] = &updated
				}
			}
			c.mu.Unlock()
			return
		}
		select {
		case <-c.ctx.Done():
			return
		case <-time.After(delay):
		}
		delay = min(30*time.Second, delay*2)
	}
}
