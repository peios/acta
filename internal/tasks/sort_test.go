package tasks

import "testing"

func TestCursorRejectsInvalidTypedValues(t *testing.T) {
	for _, c := range []Cursor{
		{Sort: "status", Direction: "asc", Number: 1, Value: "2147483648"},
		{Sort: "title", Direction: "asc", Number: 1, Value: "bad\x00title"},
		{Sort: "created", Direction: "asc", Number: 1, Value: "not a timestamp"},
	} {
		if _, e := NormalizeTaskSort(Filter{Sort: c.Sort, Direction: c.Direction, Cursor: c.Encode()}); e == nil {
			t.Fatalf("accepted invalid cursor: %#v", c)
		}
	}
}
