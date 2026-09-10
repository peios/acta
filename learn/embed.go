// Package learn embeds the agent-facing guide from its maintained documentation source.
package learn

import _ "embed"

// AgentGuide is the same Markdown reviewed and maintained in learn/.
//
//go:embed mcp-agent-guide.md
var AgentGuide string
