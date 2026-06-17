package agents

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgentSystemPrompt(t *testing.T) {
	p := AgentSystemPrompt()
	require.NotEmpty(t, p)

	// Must teach the conventions whose absence broke the OpenAI-compatible path:
	// the :::buttons widget the frontend renders, the draft-actions block, the
	// schema tool, and the canonical canvas-YAML fields / don't-invent-keys rule.
	for _, must := range []string{
		":::buttons",
		":::draft-actions",
		"superplane_component_schema",
		"superplane_app",
		"sourceId",      // canonical edge field
		"unknown field", // the don't-invent-keys rule
	} {
		assert.Contains(t, p, must, "system prompt must mention %q", must)
	}

	// Must NOT carry Anthropic-managed-only machinery the OpenAI provider lacks:
	// sub-agent researchers, mounted reference files, or the attached widget skill.
	for _, mustNot := range []string{
		"Component Researcher",
		"/mnt/session",
		"rich-ui-widgets skill",
	} {
		assert.NotContains(t, p, mustNot, "system prompt must not mention %q", mustNot)
	}
}
