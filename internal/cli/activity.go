package cli

import (
	"acta/internal/activity"
	"fmt"
	"strings"
)

func activityValue(v activity.Value) string {
	if len(v.Items) == 0 {
		if v.Text != "" {
			return v.Text
		}
		return "None"
	}
	labels := []string{}
	for _, r := range v.Items {
		labels = append(labels, r.Label)
	}
	return strings.Join(labels, ", ")
}
func renderActivity(p activity.Page) string {
	if len(p.Entries) == 0 {
		return "No activity yet."
	}
	var lines []string
	for _, e := range p.Entries {
		name := e.Actor.Username
		action := "edited " + e.Field
		switch e.Kind {
		case "task.created":
			action = "created the task"
		case "task.changed":
			switch e.Field {
			case "description":
				action = "edited the description"
			case "title":
				action = "edited the title"
			case "status_id":
				action = "changed status"
			case "parent_id":
				action = "changed parent"
			case "assignees":
				action = "changed assignees"
			}
		}
		line := fmt.Sprintf("%s  @%s %s", e.UpdatedAt.Local().Format("2006-01-02 15:04:05"), name, action)
		if e.Comment != nil {
			line = fmt.Sprintf("%s  @%s · comment %s (version %d)", e.StartedAt.Local().Format("2006-01-02 15:04:05"), name, e.ID, e.Comment.Version)
			if e.Comment.Deleted {
				line += "\n  Comment deleted"
			} else {
				line += "\n" + e.Comment.Body
			}
			if e.Comment.Edited {
				line += "\n  Edited"
			}
			if e.ReplyCount > 0 {
				line += fmt.Sprintf("\n  %d replies", e.ReplyCount)
			}
			if e.Comment.ReplyTo != "" {
				line += "\n  Replying to " + e.Comment.ReplyTo
			}
		}
		if e.Kind == "task.changed" && e.Field != "description" {
			line += "\n  " + activityValue(e.Before) + " → " + activityValue(e.After)
		}
		if e.Reason == "status_replaced" {
			line += " (workspace status replacement)"
		}
		if e.Count > 1 {
			line += fmt.Sprintf(" [%d changes]", e.Count)
		}
		lines = append(lines, terminalText(line, true))
	}
	if p.More {
		lines = append(lines, "Older activity cursor: "+p.Cursor)
	}
	return strings.Join(lines, "\n\n")
}
