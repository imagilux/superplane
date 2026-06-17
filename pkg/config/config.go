package config

import (
	"fmt"
	"os"
	"strings"
)

func RabbitMQURL() (string, error) {
	URL := os.Getenv("RABBITMQ_URL")
	if URL == "" {
		return "", fmt.Errorf("RABBITMQ_URL not set")
	}

	return URL, nil
}

func UsageGRPCURL() string {
	return os.Getenv("USAGE_GRPC_URL")
}

// AnthropicAgentConfig holds the credentials and identifiers needed to talk
// to a single Anthropic managed agent. Empty values mean managed agents are
// disabled on this installation.
type AnthropicAgentConfig struct {
	APIKey        string
	AgentID       string
	EnvironmentID string
}

// LoadAnthropicAgentConfig reads the env vars for the Anthropic managed-agents
// integration. If any required value is missing, Enabled() returns false.
func LoadAnthropicAgentConfig() AnthropicAgentConfig {
	return AnthropicAgentConfig{
		APIKey:        os.Getenv("ANTHROPIC_API_KEY"),
		AgentID:       os.Getenv("ANTHROPIC_AGENT_ID"),
		EnvironmentID: os.Getenv("ANTHROPIC_ENVIRONMENT_ID"),
	}
}

// Enabled reports whether the Anthropic provider has the credentials it
// needs to run.
func (c AnthropicAgentConfig) Enabled() bool {
	return c.APIKey != "" && c.AgentID != "" && c.EnvironmentID != ""
}

// AgentProvider returns the selected agent backend, normalized and defaulting
// to "anthropic" when AGENT_PROVIDER is unset, so existing installations keep
// the managed-agents behavior. buildAgentService dispatches on this.
func AgentProvider() string {
	provider := strings.ToLower(strings.TrimSpace(os.Getenv("AGENT_PROVIDER")))
	if provider == "" {
		return ProviderAnthropic
	}
	return provider
}

const (
	ProviderAnthropic = "anthropic"
	ProviderOpenAI    = "openai"
)

// OpenAICompatibleAgentConfig holds the connection details for a generic
// OpenAI-compatible Chat Completions endpoint — a hosted service or a local
// server such as vLLM or llama.cpp — used as an alternative agent backend.
type OpenAICompatibleAgentConfig struct {
	BaseURL string
	APIKey  string
	Model   string
}

// LoadOpenAICompatibleAgentConfig reads the env vars for the OpenAI-compatible
// agent backend. APIKey is optional (many local servers need none).
func LoadOpenAICompatibleAgentConfig() OpenAICompatibleAgentConfig {
	return OpenAICompatibleAgentConfig{
		BaseURL: os.Getenv("AGENT_BASE_URL"),
		APIKey:  os.Getenv("AGENT_API_KEY"),
		Model:   os.Getenv("AGENT_MODEL"),
	}
}

// Enabled reports whether the OpenAI-compatible backend has the minimum it
// needs: a base URL and a model. The API key is optional for unauthenticated
// local endpoints.
func (c OpenAICompatibleAgentConfig) Enabled() bool {
	return strings.TrimSpace(c.BaseURL) != "" && strings.TrimSpace(c.Model) != ""
}
