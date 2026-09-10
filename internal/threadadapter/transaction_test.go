package threadadapter

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestRejectedEnvelopeCannotCommitPartialMapping(t *testing.T) {
	p := capture(1)
	p.Provider = "claude"
	state := State{RunID: p.RunID, NativeID: "native"}
	raw := `{"type":"control_response","response":{"request_id":"` + p.RunID + `/initialize","subtype":"success","response":{"fast_mode_state":"off","session_state":"unreviewed"}}}`
	next, frames, err := Map(state, p, "stdout", raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 1 || frames[0].Kind != "debug/unknown" {
		t.Fatalf("partial outputs escaped: %+v", frames)
	}
	if !reflect.DeepEqual(state, next) {
		t.Fatal("rejected input changed adapter state")
	}
}
func TestCaptureMustBeExactlyOneJSONObject(t *testing.T) {
	p := capture(1)
	for _, raw := range []string{`{"method":"remoteControl/status/changed"} trailing`, `{"method":"remoteControl/status/changed"} {}`, `null`, `[]`} {
		_, frames, err := Map(State{RunID: p.RunID}, p, "stdout", raw)
		if err != nil || len(frames) != 1 || frames[0].Kind != "debug/unknown" {
			t.Fatalf("accepted malformed capture %q: %v %+v", raw, err, frames)
		}
	}
}
func TestInvalidCheckpointIsNotSilentlyDiscarded(t *testing.T) {
	p := capture(1)
	state := State{RunID: p.RunID, Configuration: object{"invalid": make(chan int)}}
	_, frames, err := Map(state, p, "stdout", `{}`)
	if err == nil || len(frames) != 0 {
		t.Fatal("silently discarded invalid checkpoint")
	}
}
func TestMappingRetainsJSONNumberLexemes(t *testing.T) {
	state, err := DecodeState([]byte(`{"configuration":{"number":9007199254740993}}`))
	if err != nil {
		t.Fatal(err)
	}
	if state.Configuration["number"] != json.Number("9007199254740993") {
		t.Fatal(state)
	}
}
