package learn

import (
	"strings"
	"testing"
)

func TestAgentGuideUsesPublishedBody(t *testing.T) {
	header, body, ok := strings.Cut(agentGuidePage, "\n---\n")
	if !ok || !strings.Contains(header, `title: "Working with Acta"`) {
		t.Fatal("published title and embedded guide title disagree")
	}
	if AgentGuide != "# Working with Acta\n\n"+strings.TrimLeft(body, "\n") {
		t.Fatal("embedded guide differs from the published body")
	}
	if strings.Contains(AgentGuide, "description: >-") {
		t.Fatal("Trail metadata leaked into the agent guide")
	}
}
