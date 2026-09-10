package tasks

import "slices"

type ViewDisplay struct {
	Mode      string   `json:"mode"`
	Columns   []string `json:"columns"`
	Sort      string   `json:"sort"`
	Direction string   `json:"direction"`
	Group     string   `json:"group"`
	Density   string   `json:"density"`
}
type ViewSettings struct {
	Board   string      `json:"board"`
	Filters ViewFilters `json:"filters"`
	Display ViewDisplay `json:"display"`
}

func NormalizeViewDisplay(d ViewDisplay) (ViewDisplay, error) {
	if d.Mode == "" {
		d.Mode = "table"
	}
	if d.Mode != "table" && d.Mode != "board" {
		return d, field("mode", "Choose table or board view.")
	}
	if d.Columns == nil {
		d.Columns = []string{"status", "assignees"}
	}
	if d.Sort == "" {
		d.Sort = "number"
	}
	if d.Direction == "" {
		d.Direction = "desc"
	}
	if d.Group == "" {
		d.Group = "none"
	}
	if d.Density == "" {
		d.Density = "comfortable"
	}
	if !slices.Contains([]string{"number", "title", "status", "created", "updated", "priority", "type", "size"}, d.Sort) {
		return d, field("sort", "Choose a supported sort field.")
	}
	if d.Direction != "asc" && d.Direction != "desc" {
		return d, field("direction", "Choose ascending or descending order.")
	}
	if !slices.Contains([]string{"none", "status", "assignee", "agents", "priority", "type", "size"}, d.Group) {
		return d, field("group", "Choose no grouping, status, assignee, agents, priority, type or size.")
	}
	if d.Density != "comfortable" && d.Density != "compact" {
		return d, field("density", "Choose comfortable or compact density.")
	}
	if len(d.Columns) > 5 {
		return d, field("columns", "Choose status, assignees, priority, type or size.")
	}
	for _, col := range d.Columns {
		if col != "status" && col != "assignees" && !IsProperty(col) {
			return d, field("columns", "Choose status, assignees, priority, type or size.")
		}
	}
	// Canonical order makes equivalent saves idempotent; number/title are always shown.
	cols := []string{}
	for _, col := range []string{"status", "assignees", "priority", "size", "type"} {
		if slices.Contains(d.Columns, col) {
			cols = append(cols, col)
		}
	}
	d.Columns = cols
	return d, nil
}
func NormalizeViewSettings(s ViewSettings) (ViewSettings, error) {
	var e error
	s.Board, e = BoardSlug(s.Board)
	if e != nil {
		return s, e
	}
	s.Filters, e = NormalizeViewFilters(s.Filters)
	if e != nil {
		return s, e
	}
	s.Display, e = NormalizeViewDisplay(s.Display)
	return s, e
}
