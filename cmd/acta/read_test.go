package main

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

// TestTailCutKeepsWholeTurns checks a transcript over the cap is sent from
// the newest turn that fits, never from mid-turn, and that the bytes left
// behind are counted; one under the cap is untouched.
func TestTailCutKeepsWholeTurns(t *testing.T) {
	turn := func(n int) [][]byte { // a prompt line then two 100-byte lines
		p := []byte(`{"turn":` + strings.Repeat("0", 5) + `,"start":true}`)
		body := []byte(`{"turn":` + strings.Repeat("x", 100) + `}`)
		return [][]byte{p, body, body}
	}
	var lines [][]byte
	for i := 0; i < 4; i++ {
		lines = append(lines, turn(i)...)
	}
	isStart := func(l []byte) bool { return strings.Contains(string(l), `"start":true`) }

	kept, skipped := tailCut(lines, 1<<20, isStart)
	if len(kept) != len(lines) || skipped != 0 {
		t.Fatalf("under the cap: kept %d of %d, skipped %d", len(kept), len(lines), skipped)
	}

	// budget covers roughly one and a half turns: the cut lands on the last turn's prompt
	kept, skipped = tailCut(lines, 350, isStart)
	if len(kept) != 3 || !isStart(kept[0]) {
		t.Fatalf("kept %d lines, first start=%v", len(kept), len(kept) > 0 && isStart(kept[0]))
	}
	var want int64
	for _, l := range lines[:9] {
		want += int64(len(l))
	}
	if skipped != want {
		t.Errorf("skipped %d, want %d", skipped, want)
	}

	// no turn boundary within the budget: nothing is cut rather than sending part of a turn
	kept, skipped = tailCut(lines, 50, func([]byte) bool { return false })
	if len(kept) != len(lines) || skipped != 0 {
		t.Errorf("without boundaries: kept %d, skipped %d", len(kept), skipped)
	}
}

// TestTrimStringsCutsOnlyLongValues checks that oversized string values are
// cut with a marker, short lines come back byte-identical, and the result is
// still JSON with the other fields intact.
func TestTrimStringsCutsOnlyLongValues(t *testing.T) {
	short := []byte(`{"a":"hello","n":1}`)
	if got := trimStrings(short, 64); string(got) != string(short) {
		t.Errorf("short line changed: %s", got)
	}
	long := `{"a":"` + strings.Repeat("é", 100) + `","keep":{"n":1,"s":"ok"}}`
	got := trimStrings([]byte(long), 50)
	var m map[string]any
	if err := json.Unmarshal(got, &m); err != nil {
		t.Fatalf("not JSON after trim: %v\n%s", err, got)
	}
	a, _ := m["a"].(string)
	if !strings.Contains(a, "cut by the Acta harness") || len(a) > 120 {
		t.Errorf("long value not cut: %q", a)
	}
	if !utf8.ValidString(a) {
		t.Errorf("cut split a UTF-8 sequence: %q", a[:60])
	}
	if keep, _ := m["keep"].(map[string]any); keep["s"] != "ok" || keep["n"] != float64(1) {
		t.Errorf("other fields changed: %v", m["keep"])
	}
}
