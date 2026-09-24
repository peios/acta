package codeagents

import (
	"reflect"
	"strings"
	"testing"

	"acta/internal/codethreads"
)

const initializationAck = `{"id":"request-id","result":{"userAgent":"Codex/1.0 (acta_code)","codexHome":"/test/.codex","platformFamily":"unix","platformOs":"linux"}}`

func TestDiagnosticLogConversion(t *testing.T) {
	const timestamp = "2026-09-15T14:09:18.937863Z"
	const codexLog = `{"timestamp":"2026-09-15T14:09:18.937863Z","level":"WARN","fields":{"message":"ignoring icon","extra":42},"target":"codex_skills::interface","span":{"name":"startup"}}`
	for _, tc := range []struct{ provider, raw, level, message, target, timestamp string }{
		{"codex", codexLog, "warn", "ignoring icon", "codex_skills::interface", timestamp},
		{"claude", timestamp + " [ERROR] Could not load plugin", "error", "Could not load plugin", "", timestamp},
		{"claude", "[DEBUG] Loading configuration", "debug", "Loading configuration", "", ""},
		{"claude", timestamp + " [WARNING] Check configuration", "warn", "Check configuration", "", timestamp},
	} {
		t.Run(tc.provider+tc.level, func(t *testing.T) {
			var adapter Adapter = Codex{}
			if tc.provider == "claude" {
				adapter = Claude{}
			}
			source := codethreads.Payload{Type: "debug.unknown", Provider: tc.provider, Stream: "stderr", Raw: tc.raw}
			want := source
			want.Type, want.Level, want.Message, want.Target, want.Timestamp = "debug.log", tc.level, tc.message, tc.target, tc.timestamp
			for i := 0; i < 2; i++ { // Fresh replay has the same result and retains every raw field.
				if got := adapter.NewConverter().Convert(source); !reflect.DeepEqual(got, []codethreads.Payload{want}) {
					t.Fatalf("got %#v, want %#v", got, want)
				}
			}
			source.Stream = "stdout"
			if got := adapter.NewConverter().Convert(source); !reflect.DeepEqual(got, []codethreads.Payload{source}) {
				t.Fatal("protocol output converted to log")
			}
		})
	}
	for _, raw := range []string{
		"plain stderr", "[UNKNOWN] future diagnostic", "bad-date [WARN] message", "[WARN] ",
		`{"type":"system","subtype":"hook_response","stderr":"[WARN] hook text"}`,
		strings.Replace(codexLog, `"WARN"`, `"NEW_LEVEL"`, 1),
		strings.Replace(codexLog, timestamp, "bad-date", 1),
		strings.Replace(codexLog, `"ignoring icon"`, `42`, 1),
		strings.Replace(codexLog, `"ignoring icon"`, `" "`, 1),
		codexLog[:len(codexLog)-1],
	} {
		for _, provider := range []string{"codex", "claude"} {
			var adapter Adapter = Codex{}
			if provider == "claude" {
				adapter = Claude{}
			}
			source := codethreads.Payload{Type: "debug.unknown", Provider: provider, Stream: "stderr", Raw: raw}
			if got := adapter.NewConverter().Convert(source); !reflect.DeepEqual(got, []codethreads.Payload{source}) {
				t.Fatalf("unknown diagnostic changed: %s %s", provider, raw)
			}
		}
	}
}

func TestCodexConverterDropsRemoteControlMethods(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		drop bool
	}{
		{`{"method":"remoteControl/status/changed","params":{}}`, true},
		{`{"id":"request","method":"remoteControl/future/event","params":{"anything":true}}`, true},
		{`{"method":"remoteControl/"}`, true},
		{`{"method":"remoteControl"}`, false},
		{`{"method":"remoteControlElse/status"}`, false},
		{`{"method":"thread/started","params":{"text":"remoteControl/status/changed"}}`, false},
		{`{"method":"RemoteControl/status/changed"}`, false},
		{`{"method":42}`, false},
		{`{"method":"remoteControl/status/changed"`, false},
	} {
		source := codethreads.Payload{Type: "debug.unknown", Provider: "codex", Stream: "stdout", Raw: tc.raw}
		got := (Codex{}).NewConverter().Convert(source)
		if tc.drop {
			if len(got) != 0 {
				t.Fatal("remote control event retained", tc.raw)
			}
		} else if !reflect.DeepEqual(got, []codethreads.Payload{source}) {
			t.Fatal("unrelated event dropped", tc.raw)
		}
		if tc.drop {
			source.Stream = "stderr"
			if got := (Codex{}).NewConverter().Convert(source); !reflect.DeepEqual(got, []codethreads.Payload{source}) {
				t.Fatal("stderr dropped")
			}
			source.Stream = "stdout"
			source.Provider = "claude"
			if got := (Claude{}).NewConverter().Convert(source); !reflect.DeepEqual(got, []codethreads.Payload{source}) {
				t.Fatal("Claude output dropped")
			}
		}
	}
}

func TestCodexConverterDropsOnlyInitializationAcknowledgements(t *testing.T) {
	for _, raw := range []string{initializationAck, strings.Replace(initializationAck, `"request-id"`, `42`, 1)} {
		source := codethreads.Payload{Type: "debug.unknown", Provider: "codex", Stream: "stdout", Raw: raw}
		if got := (Codex{}).NewConverter().Convert(source); len(got) != 0 {
			t.Fatal("initialization acknowledgement retained", got)
		}
	}
	for _, raw := range []string{
		`invalid`, `null`, `{"id":"id","result":{"thread":{"id":"thread"}}}`,
		`{"id":"id","error":{"message":"initialization failed"}}`,
		strings.Replace(initializationAck, `"id":"request-id"`, `"id":null`, 1),
		strings.Replace(initializationAck, `"platformOs":"linux"`, `"platformOs":null`, 1),
		strings.Replace(initializationAck, `"platformOs":"linux"`, `"platformOs":123`, 1),
		strings.Replace(initializationAck, `"platformOs":"linux"`, `"futureField":"inspect me"`, 1),
		strings.Replace(initializationAck, `"id":"request-id"`, `"method":"notification","id":"request-id"`, 1),
		strings.Replace(initializationAck, `"platformOs":"linux"`, `"platformOs":"linux","extra":"new information"`, 1),
	} {
		source := codethreads.Payload{Type: "debug.unknown", Provider: "codex", Stream: "stdout", Raw: raw}
		if got := (Codex{}).NewConverter().Convert(source); !reflect.DeepEqual(got, []codethreads.Payload{source}) {
			t.Fatal("unrecognized output dropped", raw)
		}
	}
	for _, source := range []codethreads.Payload{
		{Type: "debug.unknown", Provider: "codex", Stream: "stderr", Raw: initializationAck},
		{Type: "debug.unknown", Provider: "claude", Stream: "stdout", Raw: initializationAck},
		{Type: "message.user", Provider: "codex", Stream: "stdout", Raw: initializationAck},
	} {
		if got := (Codex{}).NewConverter().Convert(source); !reflect.DeepEqual(got, []codethreads.Payload{source}) {
			t.Fatal("non-Codex-stdout output dropped")
		}
	}
}
