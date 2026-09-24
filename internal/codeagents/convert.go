package codeagents

import (
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"acta/internal/codethreads"
)

// Converter consumes retained provider lines in observation order. Each source
// may produce zero, one or many CAT payloads. A fresh instance is used on replay;
// afterwards that instance continues converting newly arriving live output.
// Existing host input and acknowledgement records are preserved by the host.
type Converter interface {
	Convert(codethreads.Payload) []codethreads.Payload
}

func (Claude) NewConverter() Converter { return &claudeConverter{} }
func (Codex) NewConverter() Converter  { return &codexConverter{} }

type claudeConverter struct{}
type codexConverter struct{}

// These are the schema development entry points. Unknown events remain raw
// until a CAT mapping is defined; replay itself introduces no new event types.
func (*claudeConverter) Convert(source codethreads.Payload) []codethreads.Payload {
	if source.Type == "debug.unknown" && source.Provider == "claude" && source.Stream == "stdout" {
		if hook, ok := claudeHook(source); ok {
			return []codethreads.Payload{hook}
		}
	}
	if source.Type == "debug.unknown" && source.Provider == "claude" && source.Stream == "stderr" {
		if parts := claudeLogLine.FindStringSubmatch(source.Raw); parts != nil && (parts[1] == "" || validLogTimestamp(parts[1])) && strings.TrimSpace(parts[3]) != "" {
			source.Type, source.Level, source.Message, source.Timestamp = "debug.log", logLevel(parts[2]), parts[3], parts[1]
		}
	}
	return []codethreads.Payload{source}
}
func (*codexConverter) Convert(source codethreads.Payload) []codethreads.Payload {
	if source.Type == "debug.unknown" && source.Provider == "codex" && source.Stream == "stderr" {
		var event struct {
			Timestamp string `json:"timestamp"`
			Level     string `json:"level"`
			Target    string `json:"target"`
			Fields    struct {
				Message string `json:"message"`
			} `json:"fields"`
		}
		if json.Unmarshal([]byte(source.Raw), &event) == nil && validLogTimestamp(event.Timestamp) && logLevel(event.Level) != "" && strings.TrimSpace(event.Fields.Message) != "" {
			source.Type, source.Level, source.Message = "debug.log", logLevel(event.Level), event.Fields.Message
			source.Target, source.Timestamp = event.Target, event.Timestamp
		}
	}
	if source.Type == "debug.unknown" && source.Provider == "codex" && source.Stream == "stdout" {
		if frames, ok := codexHookContext(source); ok {
			return frames
		}
		if hook, ok := codexHook(source); ok {
			return []codethreads.Payload{hook}
		}
		var event struct {
			Method string `json:"method"`
		}
		if json.Unmarshal([]byte(source.Raw), &event) == nil && strings.HasPrefix(event.Method, "remoteControl/") {
			return nil
		}
		if codexInitializationAck(source.Raw) {
			return nil
		}
	}
	return []codethreads.Payload{source}
}

// Claude's diagnostic stderr is timestamped text, separate from stream-json
// protocol events. Older versions also emitted the bracketed level alone.
var claudeLogLine = regexp.MustCompile(`^(?:(\S+) )?\[(TRACE|DEBUG|INFO|WARN|WARNING|ERROR|FATAL)\] (.*)$`)

func logLevel(level string) string {
	switch strings.ToLower(level) {
	case "warning":
		return "warn"
	case "trace", "debug", "info", "warn", "error", "fatal":
		return strings.ToLower(level)
	default:
		return ""
	}
}

func validLogTimestamp(timestamp string) bool {
	_, err := time.Parse(time.RFC3339Nano, timestamp)
	return err == nil
}

// Initialization responses have no method name. Recognize only the known
// success shape; errors, malformed values and new fields remain inspectable.
func codexInitializationAck(raw string) bool {
	var envelope map[string]json.RawMessage
	if json.Unmarshal([]byte(raw), &envelope) != nil || len(envelope) != 2 {
		return false
	}
	var id any
	if json.Unmarshal(envelope["id"], &id) != nil {
		return false
	}
	switch value := id.(type) {
	case string:
		if value == "" {
			return false
		}
	case float64:
	default:
		return false
	}
	var result map[string]json.RawMessage
	if json.Unmarshal(envelope["result"], &result) != nil || len(result) != 4 {
		return false
	}
	for _, key := range []string{"userAgent", "codexHome", "platformFamily", "platformOs"} {
		var value string
		if json.Unmarshal(result[key], &value) != nil || value == "" {
			return false
		}
	}
	return true
}
