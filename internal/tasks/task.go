// Package tasks owns transport-independent task contracts and validation.
package tasks

import (
	"acta2/internal/accounts"
	"encoding/json"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

type Board struct {
	Slug      string `json:"slug"`
	Name      string `json:"name"`
	Creation  string `json:"creation_status"`
	Completed string `json:"completed_status"`
}

// BoardSlug resolves the primary default and rejects unrecognised boards.
func BoardSlug(v string) (string, error) {
	if v == "" {
		v = "tasks"
	}
	if v != "tasks" && v != "backlog" {
		return "", field("board", "Choose tasks or backlog.")
	}
	return v, nil
}

type Status struct {
	Board string `json:"board"`
	ID    string `json:"id"`
	Name  string `json:"name"`
}
type Config struct {
	Boards           []Board  `json:"boards"`
	Prefix           string   `json:"prefix"`
	PreviousPrefixes []string `json:"previous_prefixes"`
	Priorities       []string `json:"priorities"`
	Types            []string `json:"types"`
	Sizes            []string `json:"sizes"`
	Statuses         []Status `json:"statuses"`
	Creation         string   `json:"creation_status"`
	Completed        string   `json:"completed_status"`
	Version          int64    `json:"version"`
	Revision         int64    `json:"revision"`
}
type Person struct {
	OwnerID     string   `json:"owner_id"`
	ID          string   `json:"id"`
	Username    string   `json:"username"`
	DisplayName *string  `json:"display_name"`
	Agent       bool     `json:"agent"`
	Available   bool     `json:"available"`
	Sources     []Source `json:"sources"`
}
type Source struct {
	ID        string `json:"id"`
	Reference string `json:"reference"`
	Title     string `json:"title"`
	Priority  string `json:"priority"`
	Type      string `json:"type"`
	Size      string `json:"size"`
}
type Task struct {
	Board               string           `json:"board"`
	Archived            bool             `json:"archived"`
	ArchivedAt          *time.Time       `json:"archived_at"`
	ID                  string           `json:"id"`
	WorkspaceID         string           `json:"workspace_id"`
	Number              int64            `json:"number"`
	Reference           string           `json:"reference"`
	Title               string           `json:"title"`
	Priority            string           `json:"priority"`
	Type                string           `json:"type"`
	Size                string           `json:"size"`
	Description         string           `json:"description"`
	StatusID            string           `json:"status_id"`
	ParentID            string           `json:"parent_id"`
	Assignees           []Person         `json:"assignees"`
	DescendantAssignees []Person         `json:"descendant_assignees"`
	Ancestors           []Source         `json:"ancestors"`
	Children            int              `json:"children"`
	DocumentCount       int              `json:"document_count"`
	Versions            map[string]int64 `json:"versions"`
	CreatedAt           time.Time        `json:"created_at"`
	UpdatedAt           time.Time        `json:"updated_at"`
}
type Create struct {
	Board       string   `json:"board"`
	Title       string   `json:"title"`
	Priority    string   `json:"priority"`
	Type        string   `json:"type"`
	Size        string   `json:"size"`
	Description string   `json:"description"`
	StatusID    string   `json:"status_id"`
	ParentID    string   `json:"parent_id"`
	Assignees   []string `json:"assignees"`
}

// Patch changes one independently revisioned field. Retrying a saved value is idempotent.
type Patch struct {
	Field   string          `json:"field"`
	Version int64           `json:"version"`
	Value   json.RawMessage `json:"value"`
}
type Filter struct {
	Board      string   `json:"board"`
	Archived   bool     `json:"archived"`
	Group      string   `json:"group"`
	GroupID    string   `json:"group_id"`
	Sort       string   `json:"sort"`
	Direction  string   `json:"direction"`
	Cursor     string   `json:"cursor"`
	Parent     string   `json:"parent"`
	State      string   `json:"state"`
	Query      string   `json:"q"`
	Before     int64    `json:"before"`
	Priorities []string `json:"priorities"`
	Types      []string `json:"types"`
	Sizes      []string `json:"sizes"`
	Statuses   []string `json:"statuses"`
	Assignees  []string `json:"assignees"`
	Unassigned bool     `json:"unassigned"`
}

type Page struct {
	Total  int64  `json:"total"`
	Cursor string `json:"cursor"`
	Tasks  []Task `json:"tasks"`
	More   bool   `json:"more"`
	Next   int64  `json:"next"`
}
type StatusChange struct {
	Board        string            `json:"board"`
	Priorities   []string          `json:"priorities"`
	Types        []string          `json:"types"`
	Sizes        []string          `json:"sizes"`
	Statuses     []Status          `json:"statuses"`
	Creation     string            `json:"creation_status"`
	Completed    string            `json:"completed_status"`
	Replacements map[string]string `json:"replacements"`
	Version      int64             `json:"version"`
}

var prefixPattern = regexp.MustCompile(`^[A-Z]{2,10}$`)

func Prefix(v string) (string, error) {
	v = strings.ToUpper(strings.TrimSpace(v))
	if !prefixPattern.MatchString(v) {
		return "", field("prefix", "Use 2–10 letters.")
	}
	return v, nil
}
func Title(v string) (string, error) {
	v = strings.TrimSpace(v)
	if !utf8.ValidString(v) || utf8.RuneCountInString(v) < 1 || utf8.RuneCountInString(v) > 300 {
		return "", field("title", "Use a title of 1–300 characters.")
	}
	return v, nil
}
func Description(v string) error {
	if !utf8.ValidString(v) || len(v) > 100000 {
		return field("description", "Use at most 100,000 bytes of Markdown.")
	}
	return nil
}
func field(k, v string) error { return &accounts.FieldError{Field: k, Message: v} }
func ValidateStatuses(c StatusChange) error {
	board, err := BoardSlug(c.Board)
	if err != nil {
		return err
	}
	minimum := 2
	if board == "backlog" {
		minimum = 1
	}
	if len(c.Statuses) < minimum || len(c.Statuses) > 50 {
		return field("statuses", "Use 2–50 statuses for Tasks or 1–50 for Backlog.")
	}
	ids, names := map[string]bool{}, map[string]bool{}
	for _, s := range c.Statuses {
		n := strings.TrimSpace(s.Name)
		if n == "" || utf8.RuneCountInString(n) > 60 || names[strings.ToLower(n)] || ids[s.ID] {
			return field("statuses", "Status names must be distinct and contain 1–60 characters.")
		}
		ids[s.ID] = true
		names[strings.ToLower(n)] = true
	}
	if !ids[c.Creation] || (c.Completed != "" && (c.Creation == c.Completed || !ids[c.Completed])) || (board == "tasks" && c.Completed == "") {
		return field("statuses", "Choose different creation and completed statuses.")
	}
	return nil
}
