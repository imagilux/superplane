package agents

import _ "embed"

//go:embed agent_system_prompt.md
var agentSystemPrompt string

// AgentSystemPrompt is the provider-neutral SuperPlane agent system prompt:
// persona, the superplane_app / superplane_component_schema tool usage, the
// rich-UI widget conventions (:::buttons, :::rubric, :::draft-actions, …), and
// the canvas-YAML rules. It is the system-prompt counterpart to the per-turn
// mode instructions in constants.go, used by client-side providers (e.g. the
// OpenAI-compatible one) that have no server-managed system prompt of their own.
// The Anthropic managed agent keeps its own fuller prompt (it also drives
// sub-agent researchers and the rubric outcome loop, which this omits).
func AgentSystemPrompt() string {
	return agentSystemPrompt
}
