package hyperharness

import (
	"acta2/internal/harnesspipe"
	"acta2/internal/threads"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"time"
)

func claudeSpec(t threads.Descriptor) (harnesspipe.Spec, error) {
	path, err := exec.LookPath("claude")
	if err != nil {
		return harnesspipe.Spec{}, err
	}
	path, err = filepath.Abs(path)
	args := []string{"--print", "--verbose", "--input-format", "stream-json", "--output-format", "stream-json", "--include-partial-messages", "--forward-subagent-text", "--replay-user-messages", "--permission-prompts", "host", "--permission-prompt-tool", "stdio", "--allow-dangerously-skip-permissions"}
	// Claude's native Artifact tool is off by default in SDK sessions. Opt in
	// through session settings so detached pipes need no environment/restart
	// changes. Native account, policy, disable switches and approvals still apply.
	artifacts, set := os.LookupEnv("CLAUDE_CODE_ARTIFACT")
	if !set {
		artifacts = "1"
	}
	settings, _ := json.Marshal(map[string]any{"env": map[string]string{
		"CLAUDE_CODE_ARTIFACT":           artifacts,
		"CLAUDE_CODE_ARTIFACT_AUTO_OPEN": "0",
	}})
	args = append(args, "--settings", string(settings))
	if t.RunID == t.ID {
		args = append(args, "--session-id", t.ID)
	} else {
		args = append(args, "--resume", t.ProviderID)
	}
	return harnesspipe.Spec{ThreadID: t.ID, RunID: t.RunID, Executable: path, Args: args, CWD: t.CWD}, err
}

func (c *Controller) startClaude(ctx context.Context, r *localThread) error {
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
	raw, err := c.rpc(ctx, r, "initialize", map[string]any{})
	if err != nil {
		return err
	}
	var init struct {
		State string `json:"session_state"`
	}
	if json.Unmarshal(raw, &init) != nil || (init.State != "idle" && init.State != "running" && init.State != "requires_action") {
		return errors.New("Claude returned no recognized session state")
	}
	r.Thread.ProviderID = r.Thread.ID
	version, err := c.rpc(ctx, r, "get_binary_version", map[string]any{})
	if err != nil {
		return err
	}
	var runtime struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(version, &runtime) != nil || runtime.Version == "" {
		return errors.New("Claude returned no version")
	}
	r.Thread.Runtime = &threads.Runtime{Version: runtime.Version, UserAgent: "Claude Code"}
	if r.Action == "resume" && r.Configuration != nil {
		if _, err = c.rpcID(ctx, r, "apply_flag_settings", "apply_flag_settings/resume", claudeSettingsParams(*r.Configuration)); err != nil {
			return err
		}
		if _, err = c.rpcID(ctx, r, "initialize", "initialize/resume-settings", map[string]any{}); err != nil {
			return err
		}
	}
	if _, err = c.rpc(ctx, r, "get_settings", map[string]any{}); err != nil {
		return err
	}
	if r.Action == "resume" && r.PermissionMode != "" {
		if err = c.applyPermissions(ctx, r, "resume-permissions", r.PermissionMode); err != nil {
			return err
		}
	}
	c.mu.Lock()
	current := c.records[r.Thread.ID]
	current.Thread.ProviderID = r.Thread.ProviderID
	current.Thread.Runtime = r.Thread.Runtime
	current.Thread.State = "running"
	current.Discovered = true
	current.Thread.Revision++
	err = c.save(current)
	c.mu.Unlock()
	if err != nil {
		return err
	}
	return nil
}

// Reinitialization is a read-only handshake when no hooks/options are supplied.
// Use its authoritative state instead of guessing from the last UI frame.
func (c *Controller) requireClaudeIdle(ctx context.Context, r *localThread, suffix string) error {
	raw, err := c.rpcID(ctx, r, "initialize", "initialize/idle/"+suffix, map[string]any{})
	if err != nil {
		return err
	}
	var reply struct {
		State string `json:"session_state"`
	}
	if json.Unmarshal(raw, &reply) != nil || reply.State != "idle" {
		return errors.New("wait for the active Claude turn to finish")
	}
	return nil
}

func (c *Controller) claudeMessage(ctx context.Context, r *localThread, id, text string, images ...threads.InputImage) error {
	raw, _ := json.Marshal(map[string]any{"type": "user", "uuid": id, "session_id": r.Thread.ProviderID, "parent_tool_use_id": nil, "message": map[string]any{"role": "user", "content": claudeInput(text, images)}})
	writeErr := c.pipe.Write(ctx, harnesspipe.WriteRequest{RunID: r.Thread.RunID, ID: r.Thread.RunID + "/message/" + id, Data: string(raw) + "\n"})
	var after int64
	for {
		frames, err := c.pipe.Read(ctx, harnesspipe.ReadRequest{ThreadID: r.Thread.ID, After: after})
		if err != nil {
			return err
		}
		for _, frame := range frames {
			after = frame.Sequence
			if frame.RunID != r.Thread.RunID || frame.Stream != "stdout" {
				continue
			}
			var m struct {
				Type    string `json:"type"`
				ID      string `json:"uuid"`
				Command string `json:"command_uuid"`
				State   string `json:"state"`
				Session string `json:"session_id"`
				Replay  bool   `json:"isReplay"`
				User    string `json:"user_message_uuid"`
			}
			if json.Unmarshal(frame.Data, &m) != nil || m.Session != r.Thread.ProviderID {
				continue
			}
			if (m.Type == "user" && m.Replay && m.ID == id) || (m.Type == "result" && m.User == id) {
				return nil
			}
			if m.Type == "command_lifecycle" && m.Command == id {
				switch m.State {
				case "queued", "started", "completed":
					return nil
				case "cancelled", "failed":
					return &providerRPCError{"Claude did not accept this message: " + m.State}
				}
			}
		}
		if len(frames) > 0 {
			continue
		}
		if writeErr != nil {
			return writeErr
		}
		ps, err := c.pipe.Processes(ctx)
		if err != nil {
			return err
		}
		for _, p := range ps {
			if p.Spec.RunID == r.Thread.RunID && p.State != "running" {
				return errors.New("Claude exited before acknowledging the message")
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}
func (c *Controller) executeClaudeSend(ctx context.Context, r *localThread, q threads.Control) {
	result := threads.Result{ID: q.ID, ThreadID: q.ThreadID, Outcome: "rejected"}
	if !r.SendAttempted {
		if !c.prepareInput(r) {
			return
		}
	}
	err := c.claudeMessage(ctx, r, q.ID, q.Text, q.Images...)
	result.Outcome = "accepted"
	if err != nil {
		result.Outcome = "uncertain"
		var rejected *providerRPCError
		if errors.As(err, &rejected) {
			result.Outcome = "rejected"
		}
		result.Error = err.Error()
	}
	c.finishOperation(r, result)
}
func (c *Controller) claudeModels(ctx context.Context, r *localThread, q threads.Control) ([]threads.ModelOption, error) {
	raw, err := c.rpcID(ctx, r, "list_models", "list_models/"+q.ID, map[string]any{})
	if err != nil {
		return nil, err
	}
	var response struct {
		Models []struct {
			ID          string   `json:"value"`
			Resolved    string   `json:"resolvedModel"`
			Name        string   `json:"displayName"`
			Description string   `json:"description"`
			Efforts     []string `json:"supportedEffortLevels"`
			Fast        bool     `json:"supportsFastMode"`
		} `json:"models"`
	}
	if json.Unmarshal(raw, &response) != nil || len(response.Models) == 0 || len(response.Models) > 256 {
		return nil, errors.New("Claude returned an invalid model catalogue")
	}
	models := []threads.ModelOption{}
	for _, native := range response.Models {
		if native.ID == "" || len(native.ID) > 256 || len(native.Resolved) > 256 || len(native.Name) > 256 || len(native.Description) > 4096 {
			return nil, errors.New("Claude returned invalid model metadata")
		}
		option := threads.ModelOption{ID: native.ID, ResolvedModel: native.Resolved, Name: native.Name, Description: native.Description, FastMode: native.Fast, Efforts: []threads.EffortOption{}}
		if option.Name == "" {
			option.Name = option.ID
		}
		for _, level := range native.Efforts {
			if level == "" || len(level) > 32 {
				return nil, errors.New("invalid Claude effort")
			}
			option.Efforts = append(option.Efforts, threads.EffortOption{ID: level, Description: "Reasoning effort: " + level})
		}
		// Claude doesn't advertise a per-model default. Empty means let the provider
		// choose, rather than inventing an effort level on a model switch.
		if native.Fast {
			option.FastDescription = "Faster responses, increased usage; subject to account availability"
		}
		// Multiple aliases can resolve to the same selectable model. Prefer an
		// explicit name over the moving "default" alias and show it once.
		duplicate := slices.IndexFunc(models, func(m threads.ModelOption) bool {
			return option.ResolvedModel != "" && m.ResolvedModel == option.ResolvedModel
		})
		if duplicate >= 0 {
			if models[duplicate].ID == "default" && option.ID != "default" {
				models[duplicate] = option
			}
		} else {
			models = append(models, option)
		}
	}
	return models, nil
}
func claudeSettingsParams(settings threads.ModelSettings) map[string]any {
	var effort any
	if settings.Effort != "" {
		effort = settings.Effort
	}
	return map[string]any{"settings": map[string]any{"model": settings.Model, "effortLevel": effort, "fastMode": settings.FastMode}}
}
func (c *Controller) executeClaudeConfigure(ctx context.Context, r *localThread, q threads.Control) {
	result := threads.Result{ID: q.ID, ThreadID: q.ThreadID, Outcome: "rejected"}
	if !r.SendAttempted {
		models, err := c.models(ctx, r, q)
		if err == nil {
			err = validateModelSettings(models, q.Settings)
		}
		if err == nil {
			err = c.requireClaudeIdle(ctx, r, "configure-"+q.ID)
		}
		if err != nil {
			result.Error = err.Error()
			c.finishOperation(r, result)
			return
		}
		if !c.prepareInput(r) {
			return
		}
	}
	_, err := c.rpcID(ctx, r, "apply_flag_settings", "apply_flag_settings/"+q.ID, claudeSettingsParams(q.Settings))
	result.Outcome = "accepted"
	if err != nil {
		result.Outcome = "uncertain"
		var rejected *providerRPCError
		if errors.As(err, &rejected) {
			result.Outcome = "rejected"
		}
		result.Error = err.Error()
	}
	// Read back applied settings, including env/org overrides; never invent an
	// effective setting from successful transport acceptance.
	if err == nil {
		init, e := c.rpcID(ctx, r, "initialize", "initialize/configure/"+q.ID, map[string]any{})
		if e == nil {
			var info struct {
				Fast   string `json:"fast_mode_state"`
				Reason string `json:"fast_mode_disabled_reason"`
			}
			_ = json.Unmarshal(init, &info)
			if q.Settings.FastMode && info.Fast != "on" {
				result.Outcome = "rejected"
				result.Error = claudeFastModeError(info.Fast, info.Reason)
			}
			var settings json.RawMessage
			settings, e = c.rpcID(ctx, r, "get_settings", "get_settings/configure/"+q.ID, map[string]any{})
			if e == nil {
				var response struct {
					Applied struct {
						Model  string `json:"model"`
						Effort string `json:"effort"`
					} `json:"applied"`
				}
				if json.Unmarshal(settings, &response) != nil || response.Applied.Model == "" {
					e = errors.New("Claude settings could not be read back")
				} else {
					confirmed := threads.ModelSettings{Model: response.Applied.Model, Effort: response.Applied.Effort, FastMode: info.Fast == "on"}
					c.mu.Lock()
					current := c.records[r.Thread.ID]
					current.Configuration = &confirmed
					_ = c.save(current)
					c.mu.Unlock()
					result.Settings = &confirmed
				}
			}
		}
		if e != nil {
			result.Outcome = "uncertain"
			result.Error = e.Error()
		}
	}
	c.finishOperation(r, result)
}

func claudeFastModeError(state, reason string) string {
	if state == "cooldown" {
		return "Claude fast mode is temporarily cooling down after a rate limit."
	}
	reasons := map[string]string{
		"extra_usage_disabled": "Fast mode requires extra usage to be enabled in your Claude account.",
		"free":                 "Fast mode is unavailable on this Claude plan.",
		"network_error":        "Claude could not check fast-mode availability. Try again when connected.",
		"not_first_party":      "Fast mode is unavailable through this Claude provider connection.",
		"disabled_by_env":      "Fast mode is disabled by the local Claude configuration.",
		"model_not_allowed":    "Fast mode is unavailable for the selected Claude model.",
		"sdk_opt_in_required":  "Claude did not accept the fast-mode opt-in.",
		"pending":              "Claude has not yet confirmed fast-mode availability.",
	}
	if message := reasons[reason]; message != "" {
		return message
	}
	return "Claude could not enable fast mode. Its effective setting remains off."
}
