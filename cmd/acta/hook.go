package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// actaBootHookScript is the Claude Code SessionStart hook the installer drops
// alongside the MCP server. It reads the URL and bearer token straight from the
// "acta" MCP server entry the same install wrote into ~/.claude.json — so it
// needs no secret of its own and tracks re-installs and token rotation — calls
// the boot endpoint, and emits the agent's memory index as additionalContext.
// It fails OPEN: any error (offline, no jq/curl, server down) exits 0 with no
// output, so a flaky network never blocks or delays starting a session.
const actaBootHookScript = `#!/usr/bin/env bash
# Claude Code SessionStart hook, installed by 'acta mcp install'.
# Injects this agent's Acta memory boot context at session start. It reads the
# URL and bearer token from the "acta" MCP server entry in ~/.claude.json, so it
# tracks 'acta mcp install' re-runs and token rotation. Fails OPEN — any error
# exits 0 with no output, so it never blocks or delays a session.
set -uo pipefail

command -v jq >/dev/null 2>&1 || exit 0
command -v curl >/dev/null 2>&1 || exit 0

cfg="$HOME/.claude.json"
[ -r "$cfg" ] || exit 0

# The server is at user scope (top-level mcpServers) but may be project-scoped.
srv="$(jq -c '(.mcpServers.acta // (.projects[]?.mcpServers.acta) // empty)' "$cfg" 2>/dev/null | head -n1)"
[ -n "$srv" ] || exit 0

url="$(printf '%s' "$srv" | jq -r '.url // empty')"
auth="$(printf '%s' "$srv" | jq -r '.headers.Authorization // empty')"
[ -n "$url" ] && [ -n "$auth" ] || exit 0

base="${url%/mcp}"
ctx="$(curl -fsS --max-time 5 -H "Authorization: $auth" "$base/api/v1/agent/boot" 2>/dev/null)" || exit 0
[ -n "$ctx" ] || exit 0

jq -nc --arg c "$ctx" '{hookSpecificOutput: {hookEventName: "SessionStart", additionalContext: $c}}'
`

// bootHookMatchers are the SessionStart sources the boot hook fires on — every
// way a session can (re)start, so memory is loaded each time.
var bootHookMatchers = []string{"startup", "resume", "clear", "compact"}

// bootHookMarker identifies the installer's hook entries in settings.json, so a
// re-install can replace them without disturbing the user's other hooks.
const bootHookMarker = "acta-boot.sh"

// installClaudeBootHook writes the boot script under ~/.claude/hooks and
// registers it in ~/.claude/settings.json for every SessionStart matcher,
// returning the script path. Re-running is idempotent.
func installClaudeBootHook() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	hooksDir := filepath.Join(home, ".claude", "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return "", err
	}
	scriptPath := filepath.Join(hooksDir, "acta-boot.sh")
	if err := os.WriteFile(scriptPath, []byte(actaBootHookScript), 0o755); err != nil {
		return "", err
	}
	settingsPath := filepath.Join(home, ".claude", "settings.json")
	if err := registerBootHook(settingsPath, scriptPath); err != nil {
		return "", err
	}
	return scriptPath, nil
}

// registerBootHook merges the SessionStart boot hook into a Claude settings.json,
// creating the file if absent and preserving everything already in it. It is
// idempotent: any prior entries pointing at the acta boot script are dropped
// before the fresh matcher set is added, so a re-install never duplicates them.
func registerBootHook(settingsPath, scriptPath string) error {
	settings := map[string]any{}
	switch data, err := os.ReadFile(settingsPath); {
	case err == nil:
		if len(strings.TrimSpace(string(data))) > 0 {
			if err := json.Unmarshal(data, &settings); err != nil {
				return fmt.Errorf("parse %s: %w (fix or remove it, then retry)", settingsPath, err)
			}
		}
	case errors.Is(err, os.ErrNotExist):
		// A fresh settings.json — start from an empty object.
	default:
		return err
	}

	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	existing, _ := hooks["SessionStart"].([]any)

	kept := make([]any, 0, len(existing)+len(bootHookMatchers))
	for _, e := range existing {
		if !hookEntryRefersTo(e, bootHookMarker) {
			kept = append(kept, e)
		}
	}
	for _, m := range bootHookMatchers {
		kept = append(kept, map[string]any{
			"matcher": m,
			"hooks": []any{map[string]any{
				"type":    "command",
				"command": scriptPath,
				"timeout": 8,
			}},
		})
	}
	hooks["SessionStart"] = kept
	settings["hooks"] = hooks

	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(settingsPath, append(out, '\n'), 0o644)
}

// hookEntryRefersTo reports whether a SessionStart entry contains a command hook
// whose command mentions needle (used to find prior acta-boot installs).
func hookEntryRefersTo(entry any, needle string) bool {
	m, ok := entry.(map[string]any)
	if !ok {
		return false
	}
	hs, _ := m["hooks"].([]any)
	for _, h := range hs {
		hm, ok := h.(map[string]any)
		if !ok {
			continue
		}
		if cmd, _ := hm["command"].(string); strings.Contains(cmd, needle) {
			return true
		}
	}
	return false
}
