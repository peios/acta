package hyperharness

import (
	"acta/internal/harnesspipe"
	"acta/internal/threads"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
)

func (c *Controller) startCodex(ctx context.Context, r *localThread) error {
	spec, err := c.spawnSpec(r.Thread)
	if err != nil {
		return err
	}
	p, err := c.pipe.Spawn(ctx, spec)
	if err != nil {
		return err
	}
	if p.State != "running" {
		return fmt.Errorf("provider process is %s: %s", p.State, p.Error)
	}
	handshake, err := c.rpc(ctx, r, "initialize", map[string]any{"clientInfo": map[string]string{"name": "acta", "version": "0.1.0", "title": "Acta"}, "capabilities": map[string]bool{"experimentalApi": true}})
	if err != nil {
		return err
	}
	var metadata struct {
		UserAgent      string `json:"userAgent"`
		PlatformFamily string `json:"platformFamily"`
		PlatformOS     string `json:"platformOs"`
	}
	if json.Unmarshal(handshake, &metadata) != nil {
		return errors.New("invalid provider initialization response")
	}
	r.Thread.Runtime = &threads.Runtime{UserAgent: metadata.UserAgent, PlatformFamily: metadata.PlatformFamily, PlatformOS: metadata.PlatformOS}
	raw, _ := json.Marshal(map[string]any{"method": "initialized"})
	if err = c.pipe.Write(ctx, harnesspipe.WriteRequest{RunID: r.Thread.RunID, ID: r.Thread.RunID + "/initialized", Data: string(raw) + "\n"}); err != nil {
		return err
	}
	method := "thread/start"
	// Explicit legacy history avoids opting into the provider's unfinished paginated resume contract.
	params := map[string]any{"cwd": r.Thread.CWD, "ephemeral": false, "historyMode": "legacy"}
	if r.Action == "resume" {
		method = "thread/resume"
		params = map[string]any{"threadId": r.Thread.ProviderID}
		if settings := r.Configuration; settings != nil {
			params["model"] = settings.Model
			tier := "default"
			if settings.FastMode {
				tier = "priority"
			}
			params["serviceTier"] = tier
		}
	}
	result, err := c.rpc(ctx, r, method, params)
	if err != nil {
		return err
	}
	var response struct {
		Thread struct {
			ID         string `json:"id"`
			Path       string `json:"path"`
			CLIVersion string `json:"cliVersion"`
		}
	}
	if json.Unmarshal(result, &response) != nil || response.Thread.ID == "" {
		return errors.New("provider returned no thread identity")
	}
	if r.Action == "resume" && response.Thread.ID != r.Thread.ProviderID {
		return errors.New("provider resumed a different thread")
	}
	r.Thread.ProviderID = response.Thread.ID
	if r.Action == "resume" && r.Configuration != nil {
		// Resume has no effort parameter. Restore all three choices through the
		// same native settings operation before advertising the thread as ready.
		if _, err = c.rpcID(ctx, r, "thread/settings/update", "thread/settings/update/resume", settingsParams(r.Thread.ProviderID, *r.Configuration)); err != nil {
			return fmt.Errorf("could not restore model settings: %w", err)
		}
	}
	r.Path = response.Thread.Path
	r.Thread.Runtime.Version = response.Thread.CLIVersion
	if r.Action == "resume" && savedUserHistory(result, r.Thread.ProviderID) {
		r.Thread.Committed = true
	}
	// Capture the identity before checking readiness. A process loss must never
	// cause an implicit second thread/start with another provider identity.
	if r.Action == "resume" && r.PermissionMode != "" {
		if err = c.applyPermissions(ctx, r, "resume-permissions", r.PermissionMode); err != nil {
			return err
		}
	}
	c.mu.Lock()
	current := c.records[r.Thread.ID]
	current.Thread.ProviderID = r.Thread.ProviderID
	current.Path = r.Path
	current.Thread.Runtime = r.Thread.Runtime
	current.Thread.Committed = current.Thread.Committed || r.Thread.Committed
	// The provider has accepted thread creation/resumption. Publish readiness
	// independently of the first user message and persistence.
	current.Discovered = true
	current.Thread.State = "running"
	current.Thread.Revision++
	err = c.save(current)
	c.mu.Unlock()
	if err != nil {
		return err
	}
	return nil
}
func providerSpec(t threads.Descriptor) (harnesspipe.Spec, error) {
	if t.Provider == "claude" {
		return claudeSpec(t)
	}
	return codexSpec(t)
}
func codexSpec(t threads.Descriptor) (harnesspipe.Spec, error) {
	path, err := exec.LookPath("codex")
	if err != nil {
		return harnesspipe.Spec{}, err
	}
	path, err = filepath.Abs(path)
	return harnesspipe.Spec{ThreadID: t.ID, RunID: t.RunID, Executable: path, Args: []string{"-c", "features.multi_agent_v2=true", "app-server", "--stdio"}, CWD: t.CWD}, err
}
