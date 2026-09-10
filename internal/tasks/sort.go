package tasks

import (
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

type Cursor struct {
	Sort      string `json:"sort"`
	Direction string `json:"direction"`
	Value     string `json:"value"`
	Number    int64  `json:"number"`
}

func (c Cursor) Encode() string {
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}
func NormalizeTaskSort(f Filter) (Filter, error) {
	d, e := NormalizeViewDisplay(ViewDisplay{Sort: f.Sort, Direction: f.Direction})
	if e != nil {
		return f, e
	}
	f.Sort, f.Direction = d.Sort, d.Direction
	if f.Before < 0 || (f.Before != 0 && (f.Sort != "number" || f.Direction != "desc" || f.Cursor != "")) {
		return f, field("before", "Use a cursor for this sort order.")
	}
	_, e = DecodeCursor(f)
	return f, e
}
func DecodeCursor(f Filter) (Cursor, error) {
	c := Cursor{Sort: f.Sort, Direction: f.Direction, Value: "0"}
	if f.Sort == "created" || f.Sort == "updated" {
		c.Value = "1970-01-01T00:00:00Z"
	}
	if f.Cursor == "" {
		return c, nil
	}
	invalid := func() (Cursor, error) { return c, field("cursor", "This page cursor is invalid. Reload the list.") }
	if len(f.Cursor) > 16384 {
		return invalid()
	}
	b, e := base64.RawURLEncoding.DecodeString(f.Cursor)
	if e != nil || json.Unmarshal(b, &c) != nil || strings.ContainsRune(c.Value, 0) || c.Number <= 0 || c.Sort != f.Sort || c.Direction != f.Direction {
		return invalid()
	}
	switch f.Sort {
	case "number":
		if _, e = strconv.ParseInt(c.Value, 10, 64); e != nil {
			return invalid()
		}
	case "status", "priority", "type", "size":
		if _, e = strconv.ParseInt(c.Value, 10, 32); e != nil {
			return invalid()
		}
	case "created", "updated":
		if _, e = time.Parse(time.RFC3339Nano, c.Value); e != nil {
			return invalid()
		}
	}
	return c, nil
}
