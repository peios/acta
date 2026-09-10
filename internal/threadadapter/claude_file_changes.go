package threadadapter

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// Only provider-reported snapshots participate. Reading the live filesystem here
// would race later edits and would make replay produce a different history.
type ClaudeFileDiff struct {
	Before, After string
	Created       bool
}
type ClaudeTurnDiff struct {
	Files      map[string]*ClaudeFileDiff
	Patches    []string
	Sequential bool
	Incomplete bool
}

func (s *State) claudeFileChanges(t *ToolCall, result object, emit func(string, object)) {
	if (t.Name != "Write" && t.Name != "Edit") || t.Status != "completed" {
		return
	}
	if s.Claude.FileDiffs == nil {
		s.Claude.FileDiffs = map[string]*ClaudeTurnDiff{}
	}
	// Completed turns already have durable turn/diff snapshots. Release their
	// source contents when another turn begins producing edits.
	for id := range s.Claude.FileDiffs {
		if id != t.Turn && s.Turns[id] == "completed" {
			delete(s.Claude.FileDiffs, id)
		}
	}
	turn := s.Claude.FileDiffs[t.Turn]
	if turn == nil {
		turn = &ClaudeTurnDiff{Files: map[string]*ClaudeFileDiff{}}
		s.Claude.FileDiffs[t.Turn] = turn
	}
	path := str(result["filePath"])
	want := str(t.Arguments["file_path"])
	if want != "" && !filepath.IsAbs(want) && filepath.IsAbs(str(s.Configuration["cwd"])) {
		want = filepath.Join(str(s.Configuration["cwd"]), want)
	}
	before, after, created, exact := claudeFileSnapshot(t.Name, result)
	if path == "" || want == "" || filepath.Clean(path) != filepath.Clean(want) || strings.ContainsAny(path, "\r\n\t\"\\") {
		exact = false
		path = ""
	} else {
		path = filepath.Clean(path)
	}
	patch := ""
	if exact {
		patch = unifiedFileDiff(path, before, after, created)
	} else if path != "" {
		patch = claudeReportedPatch(path, result)
	}
	if patch != "" || exact {
		kind := "update"
		if created {
			kind = "add"
		}
		t.Changes = []object{{"path": path, "kind": kind, "move_path": nil, "format": "unified", "diff": patch}}
		turn.Patches = append(turn.Patches, patch)
	} else {
		turn.Incomplete = true
	}
	if !exact {
		turn.Sequential = true
	} else if file := turn.Files[path]; file != nil {
		if created || file.After != before {
			// A concurrent/external edit, missing result, or reordered operation
			// breaks the proof that these changes form a single chain.
			turn.Sequential = true
		}
		file.After = after
	} else {
		turn.Files[path] = &ClaudeFileDiff{Before: before, After: after, Created: created}
	}
	mode := "combined"
	diff := ""
	if turn.Sequential {
		mode = "sequential"
		diff = strings.Join(turn.Patches, "")
	} else {
		paths := make([]string, 0, len(turn.Files))
		for path := range turn.Files {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		for _, path := range paths {
			file := turn.Files[path]
			diff += unifiedFileDiff(path, file.Before, file.After, file.Created)
		}
	}
	emit("turn/diff", object{"turn_id": t.Turn, "diff": diff, "mode": mode, "incomplete": turn.Incomplete})
}

func claudeFileSnapshot(name string, r object) (before, after string, created, exact bool) {
	// Human-modified results cannot be reconstructed from the proposed edit.
	if r["userModified"] != false {
		return
	}
	before, hasBefore := r["originalFile"].(string)
	if name == "Write" {
		after, exact = r["content"].(string)
		_, originalReported := r["originalFile"]
		created = originalReported && r["type"] == "create" && r["originalFile"] == nil
		exact = exact && (created || (r["type"] == "update" && hasBefore))
	} else if name == "Edit" && hasBefore {
		old, oldOK := r["oldString"].(string)
		replacement, newOK := r["newString"].(string)
		all, allOK := r["replaceAll"].(bool)
		if oldOK && newOK && allOK && old != "" {
			count := strings.Count(before, old)
			if count == 1 || (all && count > 0) {
				after = strings.ReplaceAll(before, old, replacement)
				exact = true
			}
		}
	}
	exact = exact && !strings.Contains(before, "\x00") && !strings.Contains(after, "\x00")
	if exact && len(list(r["structuredPatch"])) > 0 {
		exact = claudePatchMatches(before, after, list(r["structuredPatch"]))
	}
	return
}

// Cross-check any supplied line patch against the reconstructed snapshots. The
// snapshots own exact newline bytes; hunk lines also have to agree with them.
func claudePatchMatches(before, after string, hunks []any) bool {
	a, b := fileLines(before), fileLines(after)
	x, y := 0, 0
	for _, v := range hunks {
		h := obj(v)
		values := make([]int, 4)
		for i, key := range []string{"oldStart", "oldLines", "newStart", "newLines"} {
			n, ok := h[key].(json.Number)
			if !ok {
				return false
			}
			value, err := n.Int64()
			if err != nil || value < 0 || value > int64(max(len(a), len(b))+1) {
				return false
			}
			values[i] = int(value)
		}
		oldStart, oldCount, newStart, newCount := values[0], values[1], values[2], values[3]
		if oldCount > 0 {
			oldStart--
		}
		if newCount > 0 {
			newStart--
		}
		if oldStart < x || newStart < y || oldStart > len(a) || newStart > len(b) || oldStart-x != newStart-y {
			return false
		}
		for x < oldStart {
			if a[x] != b[y] {
				return false
			}
			x++
			y++
		}
		lines, ok := h["lines"].([]any)
		if !ok {
			return false
		}
		for _, value := range lines {
			line, ok := value.(string)
			if !ok || line == "" {
				return false
			}
			if line == "\\ No newline at end of file" {
				continue
			}
			if line[0] != ' ' && line[0] != '-' && line[0] != '+' {
				return false
			}
			if line[0] != '+' {
				if x >= len(a) || strings.TrimSuffix(a[x], "\n") != line[1:] {
					return false
				}
				x++
			}
			if line[0] != '-' {
				if y >= len(b) || strings.TrimSuffix(b[y], "\n") != line[1:] {
					return false
				}
				y++
			}
		}
		if x-oldStart != oldCount || y-newStart != newCount {
			return false
		}
	}
	return strings.Join(a[x:], "") == strings.Join(b[y:], "")
}

// A reviewed structured patch is still useful when its full base is unavailable.
// It is displayed verbatim, never applied speculatively to a different snapshot.
func claudeReportedPatch(path string, r object) string {
	hunks := list(r["structuredPatch"])
	if len(hunks) == 0 {
		return ""
	}
	var out strings.Builder
	name := strings.TrimPrefix(path, "/")
	fmt.Fprintf(&out, "--- %s\n+++ %s\n", diffPath("a/"+name), diffPath("b/"+name))
	for _, v := range hunks {
		h := obj(v)
		for _, key := range []string{"oldStart", "oldLines", "newStart", "newLines"} {
			if number(h[key]) == nil || numeric(h[key]) < 0 || numeric(h[key]) != float64(int64(numeric(h[key]))) {
				return ""
			}
		}
		fmt.Fprintf(&out, "@@ -%s,%s +%s,%s @@\n", strNumber(h["oldStart"]), strNumber(h["oldLines"]), strNumber(h["newStart"]), strNumber(h["newLines"]))
		lines, ok := h["lines"].([]any)
		if !ok || len(lines) == 0 {
			return ""
		}
		for _, v := range lines {
			line, ok := v.(string)
			if !ok || line == "" || strings.Contains(line, "\n") || !strings.ContainsRune(" +-\\", rune(line[0])) {
				return ""
			}
			out.WriteString(line + "\n")
		}
	}
	return out.String()
}
