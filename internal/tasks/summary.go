package tasks

// Summary deliberately excludes descriptions and recursive assignment sources.
type Summary struct {
	Board       string   `json:"board"`
	Archived    bool     `json:"archived"`
	ID          string   `json:"id"`
	WorkspaceID string   `json:"workspace_id"`
	Reference   string   `json:"reference"`
	Title       string   `json:"title"`
	Priority    string   `json:"priority"`
	Type        string   `json:"type"`
	Size        string   `json:"size"`
	Status      Status   `json:"status"`
	ParentID    string   `json:"parent_id"`
	Assignees   []Person `json:"assignees"`
	Children    int      `json:"children"`
}
type SummaryPage struct {
	Tasks  []Summary `json:"tasks"`
	Total  int64     `json:"total"`
	More   bool      `json:"more"`
	Cursor string    `json:"cursor"`
}
type Detail struct {
	Task
	Status   Status      `json:"status"`
	Subtasks SummaryPage `json:"subtasks"`
}

func Summarize(p Page, c Config) SummaryPage {
	out := SummaryPage{Tasks: []Summary{}, Total: p.Total, More: p.More, Cursor: p.Cursor}
	names := map[string]string{}
	for _, s := range c.Statuses {
		names[s.ID] = s.Name
	}
	for _, t := range p.Tasks {
		out.Tasks = append(out.Tasks, Summary{Board: t.Board, Archived: t.Archived, ID: t.ID, WorkspaceID: t.WorkspaceID, Reference: t.Reference, Title: t.Title, Priority: t.Priority, Type: t.Type, Size: t.Size, Status: Status{ID: t.StatusID, Name: names[t.StatusID], Board: t.Board}, ParentID: t.ParentID, Assignees: t.Assignees, Children: t.Children})
	}
	return out
}
