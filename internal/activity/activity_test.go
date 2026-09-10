package activity

import (
	"testing"
	"time"
)

func TestSlidingGrouping(t *testing.T) {
	base := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	edit := Change{Kind: "task.changed", Field: "title"}
	last := base
	for _, second := range []int{59, 118, 177} {
		at := base.Add(time.Duration(second) * time.Second)
		if !CanGroup(edit, edit, "a", "a", at, last) {
			t.Fatal("sliding window failed", second)
		}
		last = at
	}
	if CanGroup(edit, edit, "a", "a", last.Add(61*time.Second), last) {
		t.Fatal("expired group merged")
	}
	if !CanGroup(edit, edit, "a", "a", last.Add(60*time.Second), last) {
		t.Fatal("boundary should merge")
	}
	for _, next := range []Change{{Kind: "comment.created"}, {Kind: "task.changed", Field: "description"}, {Kind: "task.changed", Field: "title", Reason: "other"}} {
		if CanGroup(edit, next, "a", "a", last, last) {
			t.Fatal("different change merged", next)
		}
	}
	if CanGroup(edit, edit, "b", "a", last, last) || CanGroup(edit, edit, "a", "a", last.Add(-time.Second), last) {
		t.Fatal("actor or backwards time merged")
	}
}
