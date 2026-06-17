package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAgentProvider(t *testing.T) {
	t.Setenv("AGENT_PROVIDER", "")
	assert.Equal(t, ProviderAnthropic, AgentProvider(), "defaults to anthropic when unset")

	t.Setenv("AGENT_PROVIDER", "OpenAI")
	assert.Equal(t, ProviderOpenAI, AgentProvider(), "case-insensitive")

	t.Setenv("AGENT_PROVIDER", "  anthropic  ")
	assert.Equal(t, ProviderAnthropic, AgentProvider(), "trimmed")
}

func TestOpenAICompatibleAgentConfigEnabled(t *testing.T) {
	assert.False(t, OpenAICompatibleAgentConfig{}.Enabled(), "empty is disabled")
	assert.False(t, OpenAICompatibleAgentConfig{BaseURL: "http://localhost:8081/v1"}.Enabled(), "model is required")
	assert.False(t, OpenAICompatibleAgentConfig{Model: "gemma"}.Enabled(), "base URL is required")
	assert.True(t,
		OpenAICompatibleAgentConfig{BaseURL: "http://localhost:8081/v1", Model: "gemma"}.Enabled(),
		"base URL + model is enough; API key optional",
	)
}
