// Package threadadapter translates provider captures into atomic Acta frame bundles.
package threadadapter

import (
	"acta2/internal/threads"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
)

// Map never drops raw evidence. Unsupported shapes stay unknown. A recognized
// but invalid output is a local processing error, with no partially applied state.
func Map(state State, p threads.ProviderFrame, stream, text string) (State, []threads.Frame, error) {
	next, err := state.clone()
	if err != nil {
		return state, nil, err
	}
	if next.RunID != p.RunID {
		history := next.backgroundHistory()
		next = State{Lanes: next.Lanes, BackgroundHistory: history, RunID: p.RunID, NativeID: state.NativeID, Turns: map[string]string{}, Messages: map[string]Message{}, Submissions: state.Submissions, SubmissionTexts: state.SubmissionTexts, SubmissionImages: state.SubmissionImages}
	}
	if next.Turns == nil {
		next.Turns = map[string]string{}
	}
	if next.Messages == nil {
		next.Messages = map[string]Message{}
	}
	debug := object{"raw": text, "stream": stream, "reason": "Provider frame is not recognized."}
	kind := "debug/unknown"
	var source object
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.UseNumber()
	parseErr := decoder.Decode(&source)
	if parseErr == nil {
		var extra any
		if source == nil || decoder.Decode(&extra) != io.EOF {
			parseErr = errors.New("capture must contain exactly one JSON object")
		}
	}
	var when *time.Time
	if t, ok := stamp(source["emittedAtMs"], true).(time.Time); ok {
		when = &t
	}
	var outputs []threads.Frame
	laneID := ""
	emit := func(kind string, data object) {
		f := threads.NewFrame(p, kind, data)
		f.LaneID = laneID
		f.OccurredAt = when
		outputs = append(outputs, f)
	}
	if stream == "stderr" {
		kind = "debug/provider-diagnostic"
		debug["reason"] = "Provider diagnostic output."
	} else if parseErr == nil {
		var reason string
		if handled, k, why := next.convertLanes(p, source, &laneID, emit); handled {
			kind, reason = k, why
		} else if p.Provider == "claude" {
			kind, reason = next.convertClaude(p, source, emit)
		} else if p.Provider == "codex" {
			kind, reason = next.convertCodex(p, source, emit)
		} else {
			reason = "Unsupported provider."
		}
		debug["reason"] = reason
	}
	// A rejected envelope is atomic: neither provisional output nor speculative
	// state survives, even when a handler discovered the problem after emitting.
	if kind == "debug/unknown" || stream == "stderr" {
		next = state
		outputs = nil
	}
	if p.Provider == "claude" && stream == "stdout" && parseErr == nil {
		if raw, redacted := claudeDebugCopy(source, p.RunID); redacted {
			debug["raw"] = raw
			debug["redacted"] = true
			debug["reason"] = str(debug["reason"]) + " Local configuration details are withheld from server diagnostics."
		}
	}
	if kind == "debug/local" {
		debug["processing_error"] = providerError(source["error"])
		if p.Provider == "claude" {
			debug["processing_error"] = providerError(obj(source["response"])["error"])
			if debug["redacted"] == true && debug["processing_error"] != nil {
				debug["processing_error"] = providerError("Local provider configuration query failed; details withheld.")
			}
		}
	}
	if len(outputs) > 0 {
		delete(debug, "processing_error")
		kind = "debug/resolved"
		refs := []threads.OutputReference{}
		for i := range outputs {
			outputs[i].OutputIndex = i + 1
			refs = append(refs, threads.OutputReference{OutputIndex: i + 1, Kind: outputs[i].Kind})
		}
		debug["outputs"] = refs
	}
	first := threads.NewFrame(p, kind, debug)
	first.LaneID = laneID
	first.OccurredAt = when
	bundle := append([]threads.Frame{first}, outputs...)
	if err := threads.ValidateBundle(bundle); err != nil {
		// Never retry a different classification under this same capture identity.
		next = state
		first = threads.NewFrame(p, "debug/local", object{"raw": debug["raw"], "stream": "stdout", "reason": "Recognized provider data could not be normalized.", "processing_error": object{"code": "mapping_failed", "message": err.Error(), "details": nil}})
		if debug["redacted"] == true {
			var data object
			_ = json.Unmarshal(first.Data, &data)
			data["redacted"] = true
			data["processing_error"] = providerError("Provider configuration could not be normalized; local details withheld.")
			first = threads.NewFrame(p, "debug/local", data)
		}
		first.OccurredAt = when
		bundle = []threads.Frame{first}
		if err = threads.ValidateBundle(bundle); err != nil {
			return state, nil, err
		}
	}
	return next, bundle, nil
}
