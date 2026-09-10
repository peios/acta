package tasks

import (
	"github.com/google/uuid"
	"slices"
	"strings"
	"unicode/utf8"
)

type ViewFilters struct {
	Priorities []string `json:"priorities"`
	Types      []string `json:"types"`
	Sizes      []string `json:"sizes"`
	Statuses   []string `json:"statuses"`
	Assignees  []string `json:"assignees"`
	Unassigned bool     `json:"unassigned"`
}
type View struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	ViewSettings
	Version int64 `json:"version"`
}

func ViewName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 60 {
		return "", field("name", "Use a name of 1–60 characters.")
	}
	return name, nil
}
func ValidateSelections(statuses, assignees []string) error {
	for key, ids := range map[string][]string{"statuses": statuses, "assignees": assignees} {
		if len(ids) > 100 {
			return field(key, "Select at most 100 values.")
		}
		for _, id := range ids {
			if _, e := uuid.Parse(id); e != nil {
				return field(key, "Use valid IDs.")
			}
		}
	}
	return nil
}
func NormalizeViewFilters(f ViewFilters) (ViewFilters, error) {
	if e := ValidateSelections(f.Statuses, f.Assignees); e != nil {
		return f, e
	}
	normalize := func(ids []string) []string {
		out := make([]string, 0, len(ids))
		for _, id := range ids {
			out = append(out, uuid.MustParse(id).String())
		}
		slices.Sort(out)
		return slices.Compact(out)
	}
	if e := ValidatePropertyFilters(f.Priorities, f.Types, f.Sizes); e != nil {
		return f, e
	}
	canonical := func(v []string) []string {
		out := append([]string{}, v...)
		slices.Sort(out)
		return slices.Compact(out)
	}
	f.Priorities = canonical(f.Priorities)
	f.Types = canonical(f.Types)
	f.Sizes = canonical(f.Sizes)
	f.Statuses = normalize(f.Statuses)
	f.Assignees = normalize(f.Assignees)
	return f, nil
}
