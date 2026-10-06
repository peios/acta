package tasks

import (
	"slices"
	"testing"
)

func TestSortReleasesNaturally(t *testing.T) {
	rs := []Release{{ID: "a", Name: "2026.10"}, {ID: "b", Name: "2026.9"}, {ID: "c", Name: "Beta"}, {ID: "d", Name: "2026.8"}, {ID: "e", Name: "alpha"}, {ID: "f", Name: "2027.1"}, {ID: "g", Name: "2026.09"}}
	SortReleases(rs)
	names := []string{}
	for _, r := range rs {
		names = append(names, r.Name)
	}
	if want := []string{"2026.8", "2026.9", "2026.09", "2026.10", "2027.1", "alpha", "Beta"}; !slices.Equal(names, want) {
		t.Fatal(names)
	}
}

func TestReleaseUpdateApply(t *testing.T) {
	r := Release{Name: "2026.9", State: "planned", Description: "Notes"}
	state, empty := "frozen", ""
	got, e := ReleaseUpdate{State: &state, Version: 1}.Apply(r)
	if e != nil || got.State != "frozen" || got.Name != "2026.9" || got.Description != "Notes" {
		t.Fatal(got, e)
	}
	backwards := "planned"
	if got, e = (ReleaseUpdate{State: &backwards, Version: 2}).Apply(got); e != nil || got.State != "planned" {
		t.Fatal("backwards transition", got, e)
	}
	for _, in := range []ReleaseUpdate{{State: &empty, Version: 1}, {Name: &empty, Version: 1}, {Version: 0}} {
		if _, e = in.Apply(r); e == nil {
			t.Fatal("accepted", in)
		}
	}
	if _, e = NormalizeReleaseCreate(ReleaseCreate{Name: "x", State: "shipped"}); e == nil {
		t.Fatal("unknown state")
	}
	if c, e := NormalizeReleaseCreate(ReleaseCreate{Name: " 2026.9 "}); e != nil || c.Name != "2026.9" || c.State != "planned" {
		t.Fatal(c, e)
	}
}
