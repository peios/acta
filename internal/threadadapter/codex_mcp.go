package threadadapter

import (
	"acta/internal/threads"
	"bytes"
	"encoding/json"
	"strings"
)

func formattedJSON(v any) string {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	e.SetIndent("", "  ")
	_ = e.Encode(v)
	return strings.TrimSuffix(b.String(), "\n")
}

// MCP text results use the common tool card. Rich content stays inspectable as
// unknown rather than being silently discarded or guessed into a text renderer.
func (s *State) codexMCPTool(p threads.ProviderFrame, method string, params object, emit func(string, object)) bool {
	item := obj(params["item"])
	if (method != "item/started" && method != "item/completed") || item["type"] != "mcpToolCall" ||
		str(params["threadId"]) != s.NativeID || str(params["turnId"]) == "" {
		return false
	}
	id, server, name := str(item["id"]), str(item["server"]), str(item["tool"])
	args := obj(item["arguments"])
	status := map[string]string{"inProgress": "running", "completed": "completed", "failed": "failed"}[str(item["status"])]
	if id == "" || server == "" || name == "" || args == nil || status == "" ||
		(method == "item/started") != (status == "running") {
		return false
	}
	var output *string
	if result := item["result"]; result != nil {
		r := obj(result)
		if r == nil {
			return false
		}
		var lines []string
		if content, exists := r["content"]; exists {
			parts, ok := content.([]any)
			if !ok {
				return false
			}
			for _, v := range parts {
				b := obj(v)
				text, ok := b["text"].(string)
				if b["type"] != "text" || !ok {
					return false
				}
				lines = append(lines, text)
			}
		}
		text := strings.Join(lines, "\n")
		if structured := r["structuredContent"]; structured != nil {
			var decoded any
			d := json.NewDecoder(strings.NewReader(text))
			d.UseNumber()
			parsed := d.Decode(&decoded) == nil
			canonical, _ := json.Marshal(decoded)
			expected, _ := json.Marshal(structured)
			if !parsed || !bytes.Equal(canonical, expected) {
				if text != "" {
					text += "\n\nStructured result:\n"
				}
				text += formattedJSON(structured)
			}
		}
		output = &text
	}
	if failure := item["error"]; failure != nil {
		status = "failed"
		text := formattedJSON(failure)
		if message := str(obj(failure)["message"]); message != "" {
			text = message
		}
		if output != nil && *output != "" {
			text = *output + "\n\n" + text
		}
		output = &text
	}
	fullName := server + "/" + name
	t := s.tool(id)
	if t != nil && (t.Turn != str(params["turnId"]) || t.Name != fullName) {
		return false
	}
	if t != nil && terminalTool(t.Status) {
		return true
	}
	if t == nil {
		t = &ToolCall{ID: id, Turn: str(params["turnId"]), Name: fullName, Category: "generic", Label: fullName, Arguments: args}
		s.Tools[id] = t
	}
	t.Status = status
	t.Arguments = args
	if t.Started == nil && method == "item/started" {
		t.Started = p.ReceivedAt
	}
	if method == "item/completed" {
		t.Output = output
		t.Completed = p.ReceivedAt
		t.Duration = number(item["durationMs"])
	}
	emitTool(t, emit)
	return true
}
