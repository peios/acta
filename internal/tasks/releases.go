package tasks

import (
	"cmp"
	"errors"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// NoRelease selects tasks without a target release in filters and groups.
const NoRelease = "none"

var ErrReleaseConflict = errors.New("This release changed since you read it. Reload its current version before saving.")

// ReleaseStates are ordered by lifecycle. Any transition is allowed, including
// backwards, so a mistaken transition can be undone.
var ReleaseStates = []string{"planned", "open", "frozen", "released"}

// ReleaseRef is the compact form carried on tasks, so readers see a release's
// state without a second request.
type ReleaseRef struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Codename string `json:"codename"`
	State    string `json:"state"`
}

// Release progress counts active (unarchived) tasks targeted directly at it.
// A task is finished when it has its board's completed status.
type Release struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	Name        string    `json:"name"`
	Codename    string    `json:"codename"`
	State       string    `json:"state"`
	Description string    `json:"description"`
	Total       int64     `json:"total"`
	Finished    int64     `json:"finished"`
	Version     int64     `json:"version"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (r Release) Ref() ReleaseRef {
	return ReleaseRef{ID: r.ID, Name: r.Name, Codename: r.Codename, State: r.State}
}

type ReleaseCreate struct {
	Name        string `json:"name"`
	Codename    string `json:"codename"`
	State       string `json:"state"`
	Description string `json:"description"`
}

// ReleaseUpdate changes only the supplied fields. The whole release shares one version.
type ReleaseUpdate struct {
	Name        *string `json:"name"`
	Codename    *string `json:"codename"`
	State       *string `json:"state"`
	Description *string `json:"description"`
	Version     int64   `json:"version"`
}

func ReleaseName(v string) (string, error) {
	v = strings.TrimSpace(v)
	if !utf8.ValidString(v) || utf8.RuneCountInString(v) < 1 || utf8.RuneCountInString(v) > 60 {
		return "", field("name", "Use a release name of 1–60 characters.")
	}
	return v, nil
}
func ReleaseCodename(v string) (string, error) {
	v = strings.TrimSpace(v)
	if !utf8.ValidString(v) || utf8.RuneCountInString(v) > 60 {
		return "", field("codename", "Use a codename of at most 60 characters.")
	}
	return v, nil
}
func ReleaseState(v string) (string, error) {
	if v == "" {
		v = "planned"
	}
	if !slices.Contains(ReleaseStates, v) {
		return "", field("state", "Choose planned, open, frozen or released.")
	}
	return v, nil
}
func NormalizeReleaseCreate(in ReleaseCreate) (ReleaseCreate, error) {
	var e error
	if in.Name, e = ReleaseName(in.Name); e != nil {
		return in, e
	}
	if in.Codename, e = ReleaseCodename(in.Codename); e != nil {
		return in, e
	}
	if in.State, e = ReleaseState(in.State); e != nil {
		return in, e
	}
	return in, Description(in.Description)
}

// Apply validates the supplied fields and returns the release they produce.
func (in ReleaseUpdate) Apply(r Release) (Release, error) {
	var e error
	if in.Version < 1 {
		return r, field("version", "Supply the release's current version.")
	}
	if in.Name != nil {
		if r.Name, e = ReleaseName(*in.Name); e != nil {
			return r, e
		}
	}
	if in.Codename != nil {
		if r.Codename, e = ReleaseCodename(*in.Codename); e != nil {
			return r, e
		}
	}
	if in.State != nil {
		if *in.State == "" {
			return r, field("state", "Choose planned, open, frozen or released.")
		}
		if r.State, e = ReleaseState(*in.State); e != nil {
			return r, e
		}
	}
	if in.Description != nil {
		if e = Description(*in.Description); e != nil {
			return r, e
		}
		r.Description = *in.Description
	}
	return r, nil
}

// ValidateReleaseSelections accepts release UUIDs and NoRelease.
func ValidateReleaseSelections(ids []string) error {
	if len(ids) > 100 {
		return field("releases", "Select at most 100 values.")
	}
	for _, id := range ids {
		if id == NoRelease {
			continue
		}
		if _, e := uuid.Parse(id); e != nil {
			return field("releases", "Use release IDs or none.")
		}
	}
	return nil
}
func normalizeReleaseSelections(ids []string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id != NoRelease {
			id = uuid.MustParse(id).String()
		}
		out = append(out, id)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// SortReleases orders by name, comparing digit runs numerically
// (2026.8 < 2026.9 < 2026.10) and other text case-insensitively.
func SortReleases(rs []Release) {
	slices.SortFunc(rs, func(a, b Release) int {
		if n := NaturalCompare(a.Name, b.Name); n != 0 {
			return n
		}
		return cmp.Compare(a.ID, b.ID)
	})
}
func NaturalCompare(a, b string) int {
	a, b = strings.ToLower(a), strings.ToLower(b)
	for a != "" && b != "" {
		ca, ra := naturalChunk(a)
		cb, rb := naturalChunk(b)
		if n := compareChunks(ca, cb); n != 0 {
			return n
		}
		a, b = ra, rb
	}
	return cmp.Compare(len(a), len(b))
}

// naturalChunk splits off a maximal run of ASCII digits or of other bytes.
// UTF-8 continuation bytes are never digits, so runes are never split.
func naturalChunk(s string) (string, string) {
	digit := isDigit(s[0])
	i := 1
	for i < len(s) && isDigit(s[i]) == digit {
		i++
	}
	return s[:i], s[i:]
}
func isDigit(b byte) bool { return b >= '0' && b <= '9' }
func compareChunks(a, b string) int {
	if isDigit(a[0]) && isDigit(b[0]) {
		ta, tb := strings.TrimLeft(a, "0"), strings.TrimLeft(b, "0")
		if n := cmp.Compare(len(ta), len(tb)); n != 0 {
			return n
		}
		if n := cmp.Compare(ta, tb); n != 0 {
			return n
		}
		return cmp.Compare(len(a), len(b))
	}
	return cmp.Compare(a, b)
}
