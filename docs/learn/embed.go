// Package learn embeds Acta's canonical published documentation inputs.
package learn

import (
	_ "embed"
	"strings"
)

//go:embed site/acta.product/3--connections.topic/400--agent-guide.md
var agentGuidePage string

// AgentGuide is the published guide body with its title, without Trail metadata.
var AgentGuide = guideMarkdown(agentGuidePage)

func guideMarkdown(page string) string {
	_, body, ok := strings.Cut(page, "\n---\n")
	if !strings.HasPrefix(page, "---\n") || !ok {
		panic("agent guide is missing Trail frontmatter")
	}
	return "# Working with Acta\n\n" + strings.TrimLeft(body, "\n")
}
