// Package activity defines durable change records and their grouped presentation.
package activity

import (
	"acta/internal/accounts"
	"strconv"
	"time"
)

const GroupWindow = 60 * time.Second
const PageSize = 50

type Reference struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}
type Value struct {
	Text  string      `json:"text,omitempty"`
	Items []Reference `json:"items,omitempty"`
}
type Change struct {
	Kind   string `json:"kind"`
	Field  string `json:"field,omitempty"`
	Reason string `json:"reason,omitempty"`
	Before Value  `json:"before"`
	After  Value  `json:"after"`
}
type Actor struct {
	ID          string  `json:"id"`
	Username    string  `json:"username"`
	DisplayName *string `json:"display_name"`
	OwnerID     string  `json:"owner_id,omitempty"`
}
type Comment struct {
	Body      string `json:"body"`
	Version   int64  `json:"version"`
	Deleted   bool   `json:"deleted"`
	Edited    bool   `json:"edited"`
	ThreadID  string `json:"thread_id,omitempty"`
	ReplyTo   string `json:"reply_to,omitempty"`
	CanEdit   bool   `json:"can_edit"`
	CanDelete bool   `json:"can_delete"`
}
type Entry struct {
	Comment      *Comment `json:"comment,omitempty"`
	ReplyCount   int      `json:"reply_count,omitempty"`
	ThreadUnread bool     `json:"thread_unread,omitempty"`
	ThreadLast   string   `json:"thread_last_event,omitempty"`

	ID    string `json:"id"`
	First string `json:"first_event"`
	Last  string `json:"last_event"`
	Actor Actor  `json:"actor"`
	Change
	StartedAt time.Time `json:"started_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Count     int       `json:"count"`
	Unread    bool      `json:"unread"`
}
type Page struct {
	LatestEntry string  `json:"latest_entry,omitempty"`
	LatestEvent string  `json:"latest_event,omitempty"`
	Entries     []Entry `json:"entries"`
	More        bool    `json:"more"`
	Cursor      string  `json:"cursor"`
	Unread      bool    `json:"unread"`
}
type Seen struct {
	ID      string `json:"id"`
	Through string `json:"through"`
}

func Cursor(s string) (int64, error) {
	if s == "" {
		return 0, nil
	}
	n, e := strconv.ParseInt(s, 10, 64)
	if e != nil || n < 1 {
		return 0, &accounts.FieldError{Field: "cursor", Message: "Use the activity cursor returned by the previous page."}
	}
	return n, nil
}

// Only adjacent compatible changes can merge; every other event is a barrier.
func CanGroup(previous, next Change, actor, previousActor string, at, previousAt time.Time) bool {
	return previous.Kind == "task.changed" && next.Kind == previous.Kind && next.Field == previous.Field && next.Reason == previous.Reason && actor == previousActor && !at.Before(previousAt) && at.Sub(previousAt) <= GroupWindow
}
