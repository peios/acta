package tasks

type Conflict struct {
	Field   string `json:"field"`
	Version int64  `json:"version"`
	Value   any    `json:"value" jsonschema:"Current text value or array of direct assignee UUIDs, according to field."`
}

func (*Conflict) Error() string {
	return "This task field changed. Review its current value before retrying."
}
func NewConflict(t Task, field string) *Conflict {
	var value any
	if field == "archived" {
		value = t.Archived
	} else if field == "assignees" {
		ids := []string{}
		for _, p := range t.Assignees {
			ids = append(ids, p.ID)
		}
		value = ids
	} else {
		value = map[string]string{"priority": t.Priority, "type": t.Type, "size": t.Size, "title": t.Title, "description": t.Description, "status_id": t.StatusID, "parent_id": t.ParentID}[field]
	}
	return &Conflict{field, t.Versions[field], value}
}
