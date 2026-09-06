package agentsession

import (
	"regexp"
	"strings"
)

// A session's status lives in its title, as a marker: "[TODO] Fix the
// build", "[IN PROGRESS] …", "[DONE] …". The title is the one thing every
// reader shares — Acta's list, Claude Code's own resume picker, Codex's
// thread list — so keeping the status there means it survives a machine
// being switched off and shows up wherever the session does. Acta reads
// the marker at either end of a title (older ones were typed at the end),
// renders it as a mark before the bare title, and writes it at the start
// when it sets one. See ACT-45.

const (
	StatusTodo       = "todo"
	StatusInProgress = "in_progress"
	StatusDone       = "done"
)

// Statuses lists the statuses a picker offers, in order.
var Statuses = []string{StatusTodo, StatusInProgress, StatusDone}

var statusLead = regexp.MustCompile(`(?i)^\s*\[\s*(todo|to do|in[ _-]?progress|wip|done)\s*\]\s*`)
var statusTail = regexp.MustCompile(`(?i)\s*\[\s*(todo|to do|in[ _-]?progress|wip|done)\s*\]\s*$`)

// SplitStatus reads a title's status marker, from the start or the end,
// and returns the status ("" for none) with the bare title.
func SplitStatus(title string) (status, bare string) {
	if m := statusLead.FindStringSubmatch(title); m != nil {
		return statusOf(m[1]), strings.TrimSpace(title[len(m[0]):])
	}
	if m := statusTail.FindStringSubmatch(title); m != nil {
		return statusOf(m[1]), strings.TrimSpace(title[:len(title)-len(m[0])])
	}
	return "", strings.TrimSpace(title)
}

func statusOf(word string) string {
	switch strings.ToLower(strings.NewReplacer(" ", "", "_", "", "-", "").Replace(word)) {
	case "todo":
		return StatusTodo
	case "inprogress", "wip":
		return StatusInProgress
	case "done":
		return StatusDone
	}
	return ""
}

// StatusMarker is how a status is written in a title; "" for none.
func StatusMarker(status string) string {
	switch status {
	case StatusTodo:
		return "[TODO]"
	case StatusInProgress:
		return "[IN PROGRESS]"
	case StatusDone:
		return "[DONE]"
	}
	return ""
}

// TitleWithStatus composes a title from a status and a bare title, marker
// first; a title with no status is just the bare title.
func TitleWithStatus(status, bare string) string {
	bare = strings.TrimSpace(bare)
	m := StatusMarker(status)
	switch {
	case m == "":
		return bare
	case bare == "":
		return m
	}
	return m + " " + bare
}

// WithDefaultStatus gives a title with no marker the status a session
// starts in: in progress. A session created in Acta is being worked on by
// definition; a title that already carries a marker keeps it.
func WithDefaultStatus(title string) string {
	if s, _ := SplitStatus(title); s != "" || strings.TrimSpace(title) == "" {
		return strings.TrimSpace(title)
	}
	return TitleWithStatus(StatusInProgress, title)
}

// StatusLabel words a status for a reader.
func StatusLabel(status string) string {
	switch status {
	case StatusTodo:
		return "To do"
	case StatusInProgress:
		return "In progress"
	case StatusDone:
		return "Done"
	}
	return "No status"
}
