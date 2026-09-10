package threadadapter

import "strings"

// Claude wraps submitted slash-command echoes in these three literal tags.
// Match the complete known wrapper only; ordinary user text stays untouched.
func claudeCommandText(text string) string {
	rest := text
	values := []string{}
	for _, tag := range []string{"command-name", "command-message", "command-args"} {
		rest = strings.TrimSpace(rest)
		prefix, suffix := "<"+tag+">", "</"+tag+">"
		if !strings.HasPrefix(rest, prefix) {
			return text
		}
		value, next, ok := strings.Cut(strings.TrimPrefix(rest, prefix), suffix)
		if !ok {
			return text
		}
		values = append(values, value)
		rest = next
	}
	if strings.TrimSpace(rest) != "" || !strings.HasPrefix(values[0], "/") || strings.ContainsAny(values[0], " \t\r\n<>") || strings.TrimPrefix(values[0], "/") != values[1] {
		return text
	}
	if values[2] == "" {
		return values[0]
	}
	return values[0] + " " + values[2]
}
