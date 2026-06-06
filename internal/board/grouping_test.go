package board

import (
	"net/url"
	"testing"
	"time"
)

func TestDueBucket(t *testing.T) {
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	day := func(d int) *time.Time { x := today.AddDate(0, 0, d); return &x }

	cases := []struct {
		name string
		due  *time.Time
		want string
	}{
		{"nil", nil, "none"},
		{"yesterday", day(-1), "overdue"},
		{"last week", day(-7), "overdue"},
		{"today", day(0), "today"},
		{"tomorrow", day(1), "week"},
		{"in 7 days", day(7), "week"},
		{"in 8 days", day(8), "later"},
		{"far out", day(40), "later"},
	}
	for _, c := range cases {
		if got := DueBucket(c.due); got != c.want {
			t.Errorf("%s: DueBucket = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestIsGroupMode(t *testing.T) {
	for _, m := range []string{"milestone", "release", "priority", "type", "size", "due", "assignee", "project"} {
		if !IsGroupMode(m) {
			t.Errorf("IsGroupMode(%q) = false, want true", m)
		}
	}
	for _, m := range []string{"", "status", "bogus", "Priority"} {
		if IsGroupMode(m) {
			t.Errorf("IsGroupMode(%q) = true, want false", m)
		}
	}
}

func TestIsSubgroupMode(t *testing.T) {
	for _, s := range []string{"status", "priority", "type", "size", "due", "assignee", "project"} {
		if !IsSubgroupMode(s) {
			t.Errorf("IsSubgroupMode(%q) = false, want true", s)
		}
	}
	// milestone/release aren't sub-group axes in v1; neither is junk or "".
	for _, s := range []string{"", "milestone", "release", "bogus"} {
		if IsSubgroupMode(s) {
			t.Errorf("IsSubgroupMode(%q) = true, want false", s)
		}
	}
}

// TestNormalizeViewQuerySubgroup proves a sub-grouping is captured in a view,
// except the no-op where it matches the primary (which the board ignores).
func TestNormalizeViewQuerySubgroup(t *testing.T) {
	cases := []struct {
		mode, subgroup, want string
	}{
		{"", "priority", "subgroup=priority"}, // primary defaults to status
		{"assignee", "priority", "mode=assignee&subgroup=priority"},
		{"priority", "priority", "mode=priority"}, // subgroup == primary: dropped
		{"", "status", ""},                        // status sub of status primary: dropped
		{"", "bogus", ""},                         // unknown sub: dropped
	}
	for _, c := range cases {
		v := url.Values{}
		if c.mode != "" {
			v.Set("mode", c.mode)
		}
		v.Set("subgroup", c.subgroup)
		if got := NormalizeViewQuery(v); got != c.want {
			t.Errorf("mode=%q subgroup=%q: got %q, want %q", c.mode, c.subgroup, got, c.want)
		}
	}
}

func TestIsOrderMode(t *testing.T) {
	for _, o := range []string{"priority", "due", "title", "created"} {
		if !IsOrderMode(o) {
			t.Errorf("IsOrderMode(%q) = false, want true", o)
		}
	}
	for _, o := range []string{"", "manual", "bogus"} {
		if IsOrderMode(o) {
			t.Errorf("IsOrderMode(%q) = true, want false", o)
		}
	}
}

// TestNormalizeViewQueryOrder proves a non-manual ordering is captured in a view
// and that manual/unknown are dropped (manual is the default).
func TestNormalizeViewQueryOrder(t *testing.T) {
	cases := map[string]string{
		"priority": "order=priority",
		"title":    "order=title",
		"manual":   "",
		"bogus":    "",
	}
	for order, want := range cases {
		if got := NormalizeViewQuery(url.Values{"order": {order}}); got != want {
			t.Errorf("order=%s: got %q, want %q", order, got, want)
		}
	}
}

// TestNormalizeViewQueryGroupModes proves a saved view captures any recognised
// grouping (so a "By priority" view is possible) and drops status/unknown modes.
func TestNormalizeViewQueryGroupModes(t *testing.T) {
	cases := map[string]string{
		"priority": "mode=priority",
		"assignee": "mode=assignee",
		"due":      "mode=due",
		"status":   "", // the default — omitted
		"bogus":    "", // unknown — dropped
	}
	for mode, want := range cases {
		got := NormalizeViewQuery(url.Values{"mode": {mode}})
		if got != want {
			t.Errorf("mode=%s: NormalizeViewQuery = %q, want %q", mode, got, want)
		}
	}
}
