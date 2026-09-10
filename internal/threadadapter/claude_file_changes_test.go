package threadadapter

import (
	"acta2/internal/threads"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Git is an independent patch consumer: check generated patches reconstruct
// the exact bytes, including empty files, CRLF and missing final newlines.
func assertPatch(t *testing.T, before, after, patch string, created bool) {
	t.Helper()
	assertPatchPath(t, "note.txt", before, after, patch, created)
}

func assertPatchPath(t *testing.T, name, before, after, patch string, created bool) {
	t.Helper()
	if patch == "" && !created && before == after {
		return
	}
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if !created {
		if err := os.WriteFile(path, []byte(before), 0600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("git", "apply", "--unsafe-paths", "--whitespace=nowarn", "-")
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(patch)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("apply: %v %s\n%s", err, output, patch)
	}
	actual, err := os.ReadFile(path)
	if err != nil || string(actual) != after {
		t.Fatalf("patch result %q, want %q: %v", actual, after, err)
	}
}

func TestUnifiedFileDiffUnusualPaths(t *testing.T) {
	for _, name := range []string{"file with spaces.txt", "café 漢字.txt", "tab\tname.txt", "line\nname.txt", `quote"slash\name.txt`, "trailing space ", "-option.txt"} {
		t.Run(name, func(t *testing.T) {
			for _, created := range []bool{false, true} {
				before := "before\n"
				if created {
					before = ""
				}
				assertPatchPath(t, name, before, "after\n", unifiedFileDiff(name, before, "after\n", created), created)
			}
			patch := claudeReportedPatch(name, object{"structuredPatch": []any{object{"oldStart": json.Number("1"), "oldLines": json.Number("1"), "newStart": json.Number("1"), "newLines": json.Number("1"), "lines": []any{"-before", "+after"}}}})
			assertPatchPath(t, name, "before\n", "after\n", patch, false)
		})
	}
}

func TestUnifiedFileDiffRoundTrip(t *testing.T) {
	cases := [][2]string{{"", ""}, {"", "new\n"}, {"old", "new"}, {"line", "line\n"}, {"line\n", "line"}, {"one\r\ntwo\r\n", "one\r\n三\r\n"}, {"remove\n", ""}, {strings.Repeat("old\n", 600), strings.Repeat("new\n", 600)}}
	rng := rand.New(rand.NewSource(42))
	for n := 0; n < 80; n++ {
		var a, b strings.Builder
		for i := 0; i < 30; i++ {
			line := fmt.Sprintf("line %d\n", rng.Intn(12))
			a.WriteString(line)
			if rng.Intn(4) != 0 {
				b.WriteString(line)
			}
			if rng.Intn(4) == 0 {
				fmt.Fprintf(&b, "insert %d\n", i)
			}
		}
		cases = append(cases, [2]string{a.String(), b.String()})
	}
	for i, c := range cases {
		t.Run(fmt.Sprint(i), func(t *testing.T) { assertPatch(t, c[0], c[1], unifiedFileDiff("note.txt", c[0], c[1], false), false) })
	}
	for _, text := range []string{"", "hello", "hello\n"} {
		assertPatch(t, "", text, unifiedFileDiff("note.txt", "", text, true), true)
	}
}

func TestClaudeCombinedFileChanges(t *testing.T) {
	const id = "3345d4d5-3427-4c76-8786-20cc947341c4"
	for _, scenario := range []string{"edit", "create", "overwrite", "revert", "gap", "missing", "denied", "ambiguous", "replace-all"} {
		t.Run(scenario, func(t *testing.T) {
			s := State{RunID: id, NativeID: "native", Claude: ClaudeState{Turn: "turn"}, Tools: map[string]*ToolCall{}}
			var latest object
			apply := func(name string, result object, failed bool) {
				t.Helper()
				toolID := fmt.Sprint(len(s.Tools))
				s.Tools[toolID] = &ToolCall{ID: toolID, Turn: "turn", Name: name, Arguments: object{"file_path": "note.txt"}, Status: "pending"}
				toolLabel(s.Tools[toolID])
				m := object{"type": "user", "session_id": "native", "message": object{"content": []any{object{"type": "tool_result", "tool_use_id": toolID, "content": "Result", "is_error": failed}}}, "tool_use_result": result}
				raw, _ := json.Marshal(m)
				next, frames, err := Map(s, threads.ProviderFrame{ThreadID: id, RunID: id, Provider: "claude", Sequence: int64(len(s.Tools)), ReceivedAt: time.Now()}, "stdout", string(raw))
				if err != nil {
					t.Fatal(err)
				}
				for _, f := range frames {
					var d object
					json.Unmarshal(f.Data, &d)
					if f.Kind == "debug/unknown" || d["processing_error"] != nil {
						t.Fatalf("mapping: %s", f.Data)
					}
					if f.Kind == "turn/diff" {
						latest = d
					}
				}
				// A duplicate result must not create a second edit, even after restart.
				saved, _ := json.Marshal(next)
				s, err = DecodeState(saved)
				if err != nil {
					t.Fatal(err)
				}
				_, replay, err := Map(s, threads.ProviderFrame{ThreadID: id, RunID: id, Provider: "claude", Sequence: 99, ReceivedAt: time.Now()}, "stdout", string(raw))
				if err != nil || len(replay) != 1 {
					t.Fatalf("duplicate produced outputs: %v %+v", err, replay)
				}
			}
			edit := func(before, old, new string) object {
				return object{"filePath": "note.txt", "originalFile": before, "oldString": old, "newString": new, "replaceAll": false, "userModified": false}
			}
			before, after := "alpha\nbeta\ngamma", "ALPHA\nBETA\ngamma"
			created := false
			switch scenario {
			case "create":
				apply("Write", object{"filePath": "note.txt", "originalFile": nil, "type": "create", "content": before, "userModified": false}, false)
				created = true
			case "overwrite":
				apply("Write", object{"filePath": "note.txt", "originalFile": "previous", "type": "update", "content": before, "userModified": false}, false)
			case "denied":
				apply("Edit", edit(before, "alpha", "ALPHA"), true)
				if latest != nil {
					t.Fatal("denied edit produced changes")
				}
				return
			case "replace-all":
				r := edit("x\nx\n", "x", "y")
				r["replaceAll"] = true
				apply("Edit", r, false)
				assertPatch(t, "x\nx\n", "y\ny\n", str(latest["diff"]), false)
				return
			case "ambiguous":
				apply("Edit", edit("x\nx", "x", "y"), false)
				if latest["incomplete"] != true || latest["mode"] != "sequential" {
					t.Fatal(latest)
				}
				return
			}
			apply("Edit", edit(before, "beta", "BETA"), false)
			if len(s.Tools[fmt.Sprint(len(s.Tools)-1)].Changes) != 1 {
				t.Fatal("missing operation diff")
			}
			if scenario == "gap" {
				apply("Edit", edit("external\nBETA\ngamma", "external", "ALPHA"), false)
			} else if scenario == "missing" {
				apply("Edit", nil, false)
			} else if scenario == "revert" {
				apply("Edit", edit("alpha\nBETA\ngamma", "BETA", "beta"), false)
				after = before
			} else {
				apply("Edit", edit("alpha\nBETA\ngamma", "alpha", "ALPHA"), false)
			}
			if scenario == "gap" || scenario == "missing" {
				if latest["mode"] != "sequential" {
					t.Fatal(latest)
				}
				if (scenario == "missing") != (latest["incomplete"] == true) {
					t.Fatal(latest)
				}
				return
			}
			if latest["mode"] != "combined" || latest["incomplete"] != false {
				t.Fatal(latest)
			}
			if created {
				before = ""
			}
			if scenario == "overwrite" {
				before = "previous"
			}
			assertPatch(t, before, after, str(latest["diff"]), created)
		})
	}
}

func TestClaudeCapturedStructuredPatch(t *testing.T) {
	// Shape captured from the successful QA Edit, including its EOF marker.
	const raw = `{"filePath":"note.txt","oldString":"beta","newString":"BETA","originalFile":"alpha\nbeta\ngamma","structuredPatch":[{"oldStart":1,"oldLines":3,"newStart":1,"newLines":3,"lines":[" alpha","-beta","+BETA"," gamma","\\ No newline at end of file"]}],"userModified":false,"replaceAll":false}`
	d := json.NewDecoder(strings.NewReader(raw))
	d.UseNumber()
	var result object
	if err := d.Decode(&result); err != nil {
		t.Fatal(err)
	}
	before, after, _, exact := claudeFileSnapshot("Edit", result)
	if !exact || before != "alpha\nbeta\ngamma" || after != "alpha\nBETA\ngamma" {
		t.Fatal(before, after, exact)
	}
	assertPatch(t, before, after, claudeReportedPatch("note.txt", result), false)
	result["newString"] = "contradicts patch"
	if _, _, _, exact = claudeFileSnapshot("Edit", result); exact {
		t.Fatal("contradictory result was trusted")
	}
	result["newString"] = "BETA"
	result["userModified"] = true
	if _, _, _, exact = claudeFileSnapshot("Edit", result); exact {
		t.Fatal("human-modified proposal was trusted")
	}
	result = object{"type": "create", "content": "text", "userModified": false}
	if _, _, _, exact = claudeFileSnapshot("Write", result); exact {
		t.Fatal("missing originalFile treated as nonexistent file")
	}
}
