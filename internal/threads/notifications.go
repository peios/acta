package threads

import (
	"context"
	"encoding/json"
	"time"
)

// NotificationChange is a provider-neutral intent, applied atomically with the
// frame projection. Keys identify logical events rather than delivery attempts.
type NotificationChange struct {
	Key, RunID, LaneID, TurnID, Kind, Title string
	Resolve                                 bool
	Blocking                                bool
}

func FrameNotification(f Frame) *NotificationChange {
	var d struct {
		ApprovalID string `json:"approval_id"`
		QuestionID string `json:"question_id"`
		TurnID     string `json:"turn_id"`
		Outcome    string `json:"outcome"`
		Blocking   *bool  `json:"blocking"`
	}
	if json.Unmarshal(f.Data, &d) != nil {
		return nil
	}
	n := &NotificationChange{RunID: f.RunID, LaneID: f.LaneID, TurnID: d.TurnID, Blocking: d.Blocking == nil || *d.Blocking}
	switch f.Kind {
	case "approval/request", "approval/resolved":
		if d.ApprovalID == "" {
			return nil
		}
		n.Key = "approval/" + d.ApprovalID
		n.Kind = "attention"
		n.Title = "Approval needed"
		n.Resolve = f.Kind == "approval/resolved"
	case "question/request", "question/resolved":
		if d.QuestionID == "" {
			return nil
		}
		n.Key = "question/" + d.QuestionID
		n.Kind = "attention"
		n.Title = "Question to answer"
		n.Resolve = f.Kind == "question/resolved"
	case "turn/completed":
		if f.LaneID != "" || d.TurnID == "" {
			return nil
		}
		n.Key = "turn/" + d.TurnID
		switch d.Outcome {
		case "completed":
			n.Kind = "finished"
			n.Title = "Turn finished"
		case "failed":
			n.Kind = "failed"
			n.Title = "Turn failed"
		default:
			n.Resolve = true
		}
	default:
		return nil
	}
	return n
}

type Notification struct {
	TaskID        string    `json:"task_id,omitempty"`
	WorkspaceSlug string    `json:"workspace_slug,omitempty"`
	TaskReference string    `json:"task_reference,omitempty"`
	TaskTitle     string    `json:"task_title,omitempty"`
	ActivityID    string    `json:"activity_id,omitempty"`
	ID            string    `json:"id"`
	Revision      int64     `json:"revision"`
	ThreadID      string    `json:"thread_id"`
	LaneID        string    `json:"lane_id"`
	RunID         string    `json:"run_id"`
	Sequence      int64     `json:"sequence"`
	Kind          string    `json:"kind"`
	Title         string    `json:"title"`
	ThreadName    string    `json:"thread_name"`
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
	ThreadNotifications(context.Context, string) (NotificationPage, error)
	ReadThreadNotifications(context.Context, string, []NotificationRead) error
}
