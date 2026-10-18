// Package notifications defines the task inbox storage contract.
package notifications

import (
	"context"
	"time"
)

// Notification is a task inbox item at a specific revision.
type Notification struct {
	TaskID        string    `json:"task_id,omitempty"`
	WorkspaceSlug string    `json:"workspace_slug,omitempty"`
	TaskReference string    `json:"task_reference,omitempty"`
	TaskTitle     string    `json:"task_title,omitempty"`
	ActivityID    string    `json:"activity_id,omitempty"`
	ID            string    `json:"id"`
	Revision      int64     `json:"revision"`
	Sequence      int64     `json:"sequence"`
	Kind          string    `json:"kind"`
	Title         string    `json:"title"`
	CreatedAt     time.Time `json:"created_at"`
}

// NotificationPage combines visible inbox items with their unread counts.
type NotificationPage struct {
	Items  []Notification `json:"items"`
	Counts map[string]int `json:"counts"`
	Total  int            `json:"total"`
}

// NotificationRead acknowledges only the revision the caller has actually seen.
type NotificationRead struct {
	ID       string `json:"id"`
	Revision int64  `json:"revision"`
}

// NotificationStore filters reads by current task access and applies exact-revision acknowledgements.
type NotificationStore interface {
	Notifications(context.Context, string) (NotificationPage, error)
	ReadNotifications(context.Context, string, []NotificationRead) error
}
