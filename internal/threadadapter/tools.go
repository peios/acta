package threadadapter

// ToolCall is persisted with the adapter checkpoint. Arguments being generated
// are distinct from execution; provider results or explicit denials end a call.
type ToolCall struct {
	ID, Turn, Name, Category, Label, Status string
	Arguments                               object
	Partial                                 string
	ForegroundTaskID                        string
	BackgroundID                            string
	ProcessID                               string
	BackgroundNotified                      bool
	BackgroundEchoed                        bool
	PermissionDenial                        object
	Changes                                 []object
	Images                                  []ToolImage
	Attachments                             []ToolAttachment
	ResultReceived                          bool
	Output, Stdout, Stderr                  *string
	CWD                                     string
	ExitCode, Duration, Started, Completed  any
}

func terminalTool(status string) bool {
	return status == "completed" || status == "failed" || status == "interrupted" || status == "declined" || status == "permission_denied"
}
func (s *State) tool(id string) *ToolCall {
	if s.Tools == nil {
		s.Tools = map[string]*ToolCall{}
	}
	return s.Tools[id]
}
func emitTool(t *ToolCall, emit func(string, object)) {
	emit("tool/call", object{"tool_id": t.ID, "turn_id": t.Turn, "name": t.Name, "category": t.Category, "label": t.Label, "status": t.Status, "arguments": t.Arguments, "output": t.Output, "stdout": t.Stdout, "stderr": t.Stderr, "cwd": nullable(t.CWD), "exit_code": t.ExitCode, "duration_ms": t.Duration, "started_at": t.Started, "completed_at": t.Completed, "permission_denial": t.PermissionDenial, "changes": t.Changes, "background_task_id": nullable(t.BackgroundID), "images": t.Images, "attachments": t.Attachments})
}
func textPointer(v any) *string {
	s, ok := v.(string)
	if !ok {
		return nil
	}
	return &s
}
