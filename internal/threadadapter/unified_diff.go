package threadadapter

import (
	"fmt"
	"strings"
)

type diffLine struct {
	kind byte
	text string
}

// Git path headers use C-style byte escapes, not JSON's Unicode escapes.
// Quoting keeps tabs/newlines in a filename from becoming patch structure.
func diffPath(path string) string {
	if !strings.ContainsAny(path, " \t\n\r\"\\") && strings.IndexFunc(path, func(r rune) bool { return r < 32 || r == 127 }) < 0 {
		return path
	}
	var out strings.Builder
	out.WriteByte('"')
	for i := 0; i < len(path); i++ {
		b := path[i]
		switch {
		case b == '"' || b == '\\':
			out.WriteByte('\\')
			out.WriteByte(b)
		case b < 32 || b == 127:
			fmt.Fprintf(&out, "\\%03o", b)
		default:
			out.WriteByte(b)
		}
	}
	out.WriteByte('"')
	return out.String()
}

func fileLines(text string) []string {
	if text == "" {
		return nil
	}
	lines := strings.SplitAfter(text, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// Produce an exact unified patch, including final-newline changes. A bounded
// LCS keeps nearby unchanged lines readable; large/repetitive inputs fall back
// to replacing the changed span, with the same exact before/after semantics.
func unifiedFileDiff(path, before, after string, created bool) string {
	if before == after && !created {
		return ""
	}
	a, b := fileLines(before), fileLines(after)
	prefix, suffix := 0, 0
	for prefix < len(a) && prefix < len(b) && a[prefix] == b[prefix] {
		prefix++
	}
	for suffix < len(a)-prefix && suffix < len(b)-prefix && a[len(a)-1-suffix] == b[len(b)-1-suffix] {
		suffix++
	}
	ops := make([]diffLine, 0, len(a)+len(b))
	add := func(kind byte, lines []string) {
		for _, text := range lines {
			ops = append(ops, diffLine{kind, text})
		}
	}
	add(' ', a[:prefix])
	x, y := a[prefix:len(a)-suffix], b[prefix:len(b)-suffix]
	if len(x)+1 <= 250000/(len(y)+1) {
		width := len(y) + 1
		dp := make([]int, (len(x)+1)*width)
		for i := len(x) - 1; i >= 0; i-- {
			for j := len(y) - 1; j >= 0; j-- {
				if x[i] == y[j] {
					dp[i*width+j] = 1 + dp[(i+1)*width+j+1]
				} else {
					dp[i*width+j] = max(dp[(i+1)*width+j], dp[i*width+j+1])
				}
			}
		}
		i, j := 0, 0
		for i < len(x) || j < len(y) {
			if i < len(x) && j < len(y) && x[i] == y[j] {
				ops = append(ops, diffLine{' ', x[i]})
				i++
				j++
			} else if i < len(x) && (j == len(y) || dp[(i+1)*width+j] >= dp[i*width+j+1]) {
				ops = append(ops, diffLine{'-', x[i]})
				i++
			} else {
				ops = append(ops, diffLine{'+', y[j]})
				j++
			}
		}
	} else {
		add('-', x)
		add('+', y)
	}
	add(' ', a[len(a)-suffix:])
	var out strings.Builder
	name := strings.TrimPrefix(path, "/")
	oldPath, newPath := diffPath("a/"+name), diffPath("b/"+name)
	fmt.Fprintf(&out, "diff --git %s %s\n", oldPath, newPath)
	if created {
		out.WriteString("new file mode 100644\n")
		if len(b) == 0 {
			return out.String()
		}
		fmt.Fprintf(&out, "--- /dev/null\n+++ %s\n", newPath)
	} else {
		fmt.Fprintf(&out, "--- %s\n+++ %s\n", oldPath, newPath)
	}
	oldPos, newPos := 1, 1
	for i := 0; i < len(ops); {
		first := i
		for first < len(ops) && ops[first].kind == ' ' {
			first++
		}
		if first == len(ops) {
			break
		}
		start := max(i, first-3)
		for ; i < start; i++ {
			oldPos++
			newPos++
		}
		last := first
		for j := first + 1; j < len(ops) && j <= last+7; j++ {
			if ops[j].kind != ' ' {
				last = j
			}
		}
		end := min(len(ops), last+4)
		oldCount, newCount := 0, 0
		for _, op := range ops[start:end] {
			if op.kind != '+' {
				oldCount++
			}
			if op.kind != '-' {
				newCount++
			}
		}
		oldStart, newStart := oldPos, newPos
		if oldCount == 0 {
			oldStart--
		}
		if newCount == 0 {
			newStart--
		}
		fmt.Fprintf(&out, "@@ -%d,%d +%d,%d @@\n", oldStart, oldCount, newStart, newCount)
		for _, op := range ops[start:end] {
			out.WriteByte(op.kind)
			out.WriteString(op.text)
			if !strings.HasSuffix(op.text, "\n") {
				out.WriteString("\n\\ No newline at end of file\n")
			}
		}
		oldPos += oldCount
		newPos += newCount
		i = end
	}
	return out.String()
}
