// Package notifications defines the task inbox storage contract.
package notifications

import (
	"context"
	"time"
)

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
type NotificationPage struct {
	Items  []Notification `json:"items"`
	Counts map[string]int `json:"counts"`
	Total  int            `json:"total"`
}
type NotificationRead struct {
	ID       string `json:"id"`
	Revision int64  `json:"revision"`
}
type NotificationStore interface {
	Notifications(context.Context, string) (NotificationPage, error)
	ReadNotifications(context.Context, string, []NotificationRead) error
}
