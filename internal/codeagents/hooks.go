package codeagents

import (
	"encoding/json"
	"strings"

	"acta/internal/codethreads"
)

// Provider snapshots are self-contained. In particular Claude's progress output
// is cumulative, not a delta: concatenating it would duplicate earlier output.
func claudeHook(source codethreads.Payload) (codethreads.Payload, bool) {
	var event struct {
		Type     string  `json:"type"`
		Subtype  string  `json:"subtype"`
		ID       string  `json:"hook_id"`
		Name     string  `json:"hook_name"`
		Event    string  `json:"hook_event"`
		Stdout   *string `json:"stdout"`
		Stderr   *string `json:"stderr"`
		Output   *string `json:"output"`
		ExitCode *int    `json:"exit_code"`
		Outcome  string  `json:"outcome"`
	}
	if json.Unmarshal([]byte(source.Raw), &event) != nil || event.Type != "system" || event.ID == "" || event.Event == "" || event.Name == "" {
		return source, false
	}
	hook := &codethreads.Hook{ID: event.ID, Event: hookEvent(event.Event), Name: event.Name, Status: "running", Outputs: []codethreads.HookOutput{}}
	switch event.Subtype {
	case "hook_started":
		source.Type = "hook.started"
	case "hook_progress", "hook_response":
		if event.Stdout == nil || event.Stderr == nil || event.Output == nil {
			return source, false
		}
		for _, output := range []codethreads.HookOutput{{Kind: "stdout", Text: *event.Stdout}, {Kind: "stderr", Text: *event.Stderr}, {Kind: "output", Text: *event.Output}} {
			if output.Text != "" {
				hook.Outputs = append(hook.Outputs, output)
			}
		}
		source.Type = "hook.progress"
		if event.Subtype == "hook_response" {
			source.Type = "hook.completed"
			hook.Status = map[string]string{"success": "completed", "error": "failed", "cancelled": "cancelled"}[event.Outcome]
			if hook.Status == "" {
				return source, false
			}
			hook.ExitCode = event.ExitCode
			if hook.Status == "completed" && (event.ExitCode == nil || *event.ExitCode == 0) {
				hook.Context = claudeHookContext(event.Event, *event.Stdout)
			}
		}
	default:
		return source, false
	}
	source.Hook = hook
	return source, true
}

// This is the hook's reported context payload, not a model-consumption ack.
// Claude's SDK stream does not expose its internal hook-context attachments.
func claudeHookContext(event, stdout string) *codethreads.HookContext {
	text := strings.TrimSpace(stdout)
	if text == "" {
		return nil
	}
	var data struct {
		Continue *bool  `json:"continue"`
		Decision string `json:"decision"`
		Async    bool   `json:"async"`
		Specific *struct {
			Event              string `json:"hookEventName"`
			Context            string `json:"additionalContext"`
			PermissionDecision string `json:"permissionDecision"`
		} `json:"hookSpecificOutput"`
	}
	// JSON-looking malformed output must never be presented as injected prose.
	if strings.HasPrefix(text, "{") || json.Valid([]byte(text)) {
		if json.Unmarshal([]byte(text), &data) != nil || data.Async || (data.Continue != nil && !*data.Continue) || data.Decision == "block" || data.Specific == nil || data.Specific.Event != event {
			return nil
		}
		if data.Specific.PermissionDecision == "defer" {
			return nil
		}
		switch event {
		case "SessionStart", "Setup", "UserPromptSubmit", "UserPromptExpansion", "PreToolUse", "PostToolUse", "PostToolUseFailure", "PostToolBatch", "SubagentStart", "SubagentStop", "Stop", "PostModelSwitch":
		default:
			return nil
		}
		text = data.Specific.Context
	} else {
		switch event {
		case "SessionStart", "UserPromptSubmit", "UserPromptExpansion", "PostModelSwitch":
		default:
			return nil
		}
		text = stdout
	}
	if strings.TrimSpace(text) == "" {
		return nil
	}
	return &codethreads.HookContext{Texts: []string{text}, Source: "hook_response"}
}

// Each completed hookPrompt item is an explicit context-insertion record.
// Group fragments by run without losing their provider order or empty strings.
func codexHookContext(source codethreads.Payload) ([]codethreads.Payload, bool) {
	var event struct {
		Method string `json:"method"`
		Params struct {
			TurnID string `json:"turnId"`
			Item   struct {
				Type      string `json:"type"`
				ID        string `json:"id"`
				Fragments []struct {
					ID   string  `json:"hookRunId"`
					Text *string `json:"text"`
				} `json:"fragments"`
			} `json:"item"`
		} `json:"params"`
	}
	if json.Unmarshal([]byte(source.Raw), &event) != nil || event.Params.Item.Type != "hookPrompt" || event.Params.Item.ID == "" || event.Params.Item.Fragments == nil {
		return nil, false
	}
	if event.Method != "item/started" && event.Method != "item/completed" {
		return nil, false
	}
	for _, f := range event.Params.Item.Fragments {
		if f.ID == "" || f.Text == nil {
			return nil, false
		}
	}
	if event.Method == "item/started" {
		return nil, true
	}
	frames := []codethreads.Payload{}
	positions := map[string]int{}
	for _, f := range event.Params.Item.Fragments {
		if i, ok := positions[f.ID]; ok {
			frames[i].Hook.Context.Texts = append(frames[i].Hook.Context.Texts, *f.Text)
			continue
		}
		p := source
		p.Type, p.TurnID = "hook.context", event.Params.TurnID
		p.Hook = &codethreads.Hook{ID: f.ID, Event: "hook", Status: "unknown", Outputs: []codethreads.HookOutput{}, Context: &codethreads.HookContext{Texts: []string{*f.Text}, Source: "context_item"}}
		positions[f.ID] = len(frames)
		frames = append(frames, p)
	}
	return frames, true
}

func codexHook(source codethreads.Payload) (codethreads.Payload, bool) {
	var event struct {
		Method string `json:"method"`
		Params struct {
			ThreadID string `json:"threadId"`
			TurnID   string `json:"turnId"`
			Run      struct {
				ID            string                   `json:"id"`
				Event         string                   `json:"eventName"`
				Status        string                   `json:"status"`
				Entries       []codethreads.HookOutput `json:"entries"`
				StatusMessage string                   `json:"statusMessage"`
				HandlerType   string                   `json:"handlerType"`
				ExecutionMode string                   `json:"executionMode"`
				Scope         string                   `json:"scope"`
				Source        string                   `json:"source"`
				SourcePath    string                   `json:"sourcePath"`
				StartedAt     *int64                   `json:"startedAt"`
				CompletedAt   *int64                   `json:"completedAt"`
				DurationMS    *int64                   `json:"durationMs"`
			} `json:"run"`
		} `json:"params"`
	}
	if json.Unmarshal([]byte(source.Raw), &event) != nil {
		return source, false
	}
	run := event.Params.Run
	if event.Params.ThreadID == "" || run.ID == "" || run.Event == "" || run.Entries == nil {
		return source, false
	}
	switch event.Method {
	case "hook/started":
		if run.Status != "running" {
			return source, false
		}
		source.Type = "hook.started"
	case "hook/completed":
		switch run.Status {
		case "completed", "failed", "blocked", "stopped":
		default:
			return source, false
		}
		source.Type = "hook.completed"
	default:
		return source, false
	}
	for _, entry := range run.Entries {
		switch entry.Kind {
		case "warning", "stop", "feedback", "context", "error":
		default:
			return source, false
		}
	}
	source.TurnID = event.Params.TurnID
	source.Hook = &codethreads.Hook{ID: run.ID, Event: hookEvent(run.Event), Status: run.Status, Outputs: run.Entries, StatusMessage: run.StatusMessage, HandlerType: run.HandlerType, ExecutionMode: run.ExecutionMode, Scope: run.Scope, Source: run.Source, SourcePath: run.SourcePath, StartedAt: run.StartedAt, CompletedAt: run.CompletedAt, DurationMS: run.DurationMS}
	return source, true
}

// Normalize known equivalent event names without guessing at future semantics.
func hookEvent(event string) string {
	names := map[string]string{
		"sessionstart": "session_start", "sessionend": "session_end", "pretooluse": "pre_tool_use", "posttooluse": "post_tool_use", "posttoolusefailure": "post_tool_use_failure",
		"permissionrequest": "permission_request", "precompact": "pre_compact", "postcompact": "post_compact", "userpromptsubmit": "user_prompt_submit", "subagentstart": "subagent_start", "subagentstop": "subagent_stop", "stop": "stop", "interrupt": "interrupt",
	}
	if name := names[strings.ToLower(event)]; name != "" {
		return name
	}
	return event
}
