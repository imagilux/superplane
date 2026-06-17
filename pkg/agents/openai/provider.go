// Package openai implements agents.Provider against an OpenAI-compatible Chat
// Completions API — a hosted service or a local server such as vLLM or
// llama.cpp. Unlike Anthropic's managed agents, the endpoint is stateless, so
// this provider synthesizes sessions and the agent loop client-side.
//
// This file is the config + selection skeleton (#6). The session/streaming core
// (#7) and the tool-calling loop (#8) fill in the stubbed methods.
package openai

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/superplanehq/superplane/pkg/agents"
)

const ProviderName = "openai"

// ErrNotImplemented is returned by the provider methods that the core-chat (#7)
// and tool-calling (#8) subtasks implement.
var ErrNotImplemented = errors.New("openai: agent provider not implemented yet")

// Config describes the OpenAI-compatible endpoint.
type Config struct {
	BaseURL string
	APIKey  string // optional; many local servers need none
	Model   string
}

type Provider struct {
	baseURL string
	apiKey  string
	model   string
}

var _ agents.Provider = (*Provider)(nil)

// New validates the endpoint config and returns a Provider. BaseURL and Model
// are required; the API key is optional (unauthenticated local servers).
func New(cfg Config) (*Provider, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, fmt.Errorf("openai: BaseURL is required")
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return nil, fmt.Errorf("openai: Model is required")
	}
	return &Provider{
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		apiKey:  cfg.APIKey,
		model:   cfg.Model,
	}, nil
}

func (p *Provider) Name() string { return ProviderName }

func (p *Provider) CreateSession(ctx context.Context, opts agents.CreateSessionOptions) (*agents.CreateSessionResult, error) {
	return nil, ErrNotImplemented
}

func (p *Provider) SendMessage(ctx context.Context, providerSessionID, message string, opts agents.SendMessageOptions) error {
	return ErrNotImplemented
}

func (p *Provider) InterruptSession(ctx context.Context, providerSessionID string) error {
	return ErrNotImplemented
}

func (p *Provider) DefineOutcome(ctx context.Context, providerSessionID string, opts agents.DefineOutcomeOptions) error {
	return ErrNotImplemented
}

func (p *Provider) StreamEvents(ctx context.Context, providerSessionID string, onEvent func(agents.ProviderEvent) error) error {
	return ErrNotImplemented
}
