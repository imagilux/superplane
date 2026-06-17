// Package openai implements agents.Provider against an OpenAI-compatible Chat
// Completions API — a hosted service or a local server such as vLLM or
// llama.cpp. Unlike Anthropic's managed agents, the endpoint is stateless, so
// this provider synthesizes sessions and the agent loop client-side: each
// session keeps its own conversation history, and SendMessage runs the
// completion in a goroutine whose events StreamEvents drains.
//
// Tool calls are surfaced as custom-tool events; the worker executes them and
// calls SendCustomToolResults, which resumes the turn. The autonomous rubric
// loop (DefineOutcome) has no OpenAI-compatible equivalent and is reported
// unsupported.
package openai

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
	"github.com/superplanehq/superplane/pkg/agents"
	"github.com/superplanehq/superplane/pkg/netguard"
)

const ProviderName = "openai"

// systemPrompt frames the assistant. NOTE: this is intentionally minimal; full
// parity with the Anthropic managed agent's system prompt + tool instructions
// (see pkg/agents/anthropic.SyncDefaultAgentPrompt) is a follow-up.
const systemPrompt = "You are SuperPlane's assistant, helping users understand and edit automation canvases. Be concise and accurate. When you are unsure, say so rather than guessing."

// ErrDefineOutcomeUnsupported is returned by DefineOutcome: the autonomous
// rubric/outcome loop is a managed-agent feature with no OpenAI-compatible
// equivalent (tracked as a follow-up).
var ErrDefineOutcomeUnsupported = errors.New("openai: DefineOutcome (autonomous rubric loop) is not supported by OpenAI-compatible providers")

// ToolDefinition is a provider-neutral description of a custom tool the agent
// can call. buildAgentService sources these from the agent_tools registry; this
// package maps them to OpenAI function tools (it must not import agent_tools).
type ToolDefinition struct {
	Name        string
	Description string
	Parameters  map[string]any
}

// Config describes the OpenAI-compatible endpoint.
type Config struct {
	BaseURL string
	APIKey  string // optional; many local servers need none
	Model   string
	Tools   []ToolDefinition
	// HTTPClient is optional; when nil a client with a generous timeout is used.
	// Tests inject the httptest server's client.
	HTTPClient *http.Client
}

type Provider struct {
	baseURL    string
	apiKey     string
	model      string
	tools      []ToolDefinition
	httpClient *http.Client

	mu       sync.Mutex
	sessions map[string]*session
}

var (
	_ agents.Provider               = (*Provider)(nil)
	_ agents.ProviderSessionCleaner = (*Provider)(nil)
	_ agents.CustomToolResultSender = (*Provider)(nil)
)

// New validates the endpoint config and returns a Provider. BaseURL and Model
// are required; the API key is optional (unauthenticated local servers).
func New(cfg Config) (*Provider, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, fmt.Errorf("openai: BaseURL is required")
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return nil, fmt.Errorf("openai: Model is required")
	}
	// When no client is injected (production), default to an SSRF-guarded
	// client: the base URL is admin-supplied, so the server must not be coaxed
	// into dialing cloud-metadata / link-local / multicast addresses. Loopback
	// IS allowed here — a local model server on 127.0.0.1 (Ollama, llama.cpp) is
	// a legitimate target for a custom agent endpoint. Tests inject their own
	// client to reach httptest servers.
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = netguard.NewGuardedHTTPClientAllowingLoopback(5 * time.Minute)
	}
	return &Provider{
		baseURL:    strings.TrimRight(cfg.BaseURL, "/"),
		apiKey:     cfg.APIKey,
		model:      cfg.Model,
		tools:      cfg.Tools,
		httpClient: httpClient,
		sessions:   map[string]*session{},
	}, nil
}

func (p *Provider) Name() string { return ProviderName }

// CreateSession mints a local session id and seeds the conversation history.
// There is no upstream session to create — the endpoint is stateless.
func (p *Provider) CreateSession(_ context.Context, opts agents.CreateSessionOptions) (*agents.CreateSessionResult, error) {
	history := []chatMessage{{Role: "system", Content: systemPrompt}}
	if strings.TrimSpace(opts.InitialContext) != "" {
		history = append(history, chatMessage{Role: "system", Content: opts.InitialContext})
	}

	id := uuid.NewString()
	p.mu.Lock()
	p.sessions[id] = newSession(id, history)
	p.mu.Unlock()

	return &agents.CreateSessionResult{ProviderSessionID: id}, nil
}

// getSession returns a tracked session, or ErrProviderSessionUnavailable so the
// Service's recovery path recreates it — this is how in-memory sessions survive
// a process restart (the old id is gone; Service calls CreateSession again).
func (p *Provider) getSession(providerSessionID string) (*session, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	s, ok := p.sessions[providerSessionID]
	if !ok {
		return nil, fmt.Errorf("%w: openai session %q not found", agents.ErrProviderSessionUnavailable, providerSessionID)
	}
	return s, nil
}

func (p *Provider) SendMessage(_ context.Context, providerSessionID, message string, opts agents.SendMessageOptions) error {
	s, err := p.getSession(providerSessionID)
	if err != nil {
		return err
	}

	content := message
	if opts.ContextPreamble != "" {
		content = opts.ContextPreamble + "\n\n" + message
	}
	s.appendHistory(chatMessage{Role: "user", Content: content})

	// The completion runs in the background and is drained by StreamEvents. It is
	// deliberately detached from the SendMessage request ctx (which returns
	// immediately); InterruptSession cancels it.
	runCtx, cancel := context.WithCancel(context.Background())
	s.setCancel(cancel)
	go p.runTurn(runCtx, s)
	return nil
}

// runTurn streams one assistant turn. A plain turn emits a single
// assistant_message with the full text, then turn_completed. A tool-calling turn
// records the assistant message that requested the tools (OpenAI requires it to
// precede the results), surfaces each call, then suspends — the worker executes
// the tools and calls SendCustomToolResults, which resumes the loop.
func (p *Provider) runTurn(ctx context.Context, s *session) {
	var content, reasoning strings.Builder
	tools := newToolCallAccumulator()
	var finishReason string

	err := p.streamCompletion(ctx, s.snapshotHistory(), func(chunk chatCompletionChunk) error {
		if chunk.Error != nil && chunk.Error.Message != "" {
			return errors.New(chunk.Error.Message)
		}
		for _, choice := range chunk.Choices {
			content.WriteString(choice.Delta.Content)
			reasoning.WriteString(choice.Delta.ReasoningContent)
			reasoning.WriteString(choice.Delta.Reasoning)
			tools.add(choice.Delta.ToolCalls)
			if choice.FinishReason != "" {
				finishReason = choice.FinishReason
			}
		}
		return nil
	})

	if err != nil {
		// An interrupt (cancelled ctx) is a clean stop, not a failure.
		if errors.Is(err, context.Canceled) {
			s.enqueue(agents.ProviderEvent{Type: agents.ProviderEventTurnCompleted})
			return
		}
		s.enqueue(agents.ProviderEvent{Type: agents.ProviderEventSessionFailed, ErrorMessage: err.Error()})
		return
	}

	// Split reasoning off the answer: the dedicated reasoning_content delta field
	// plus any inline <think>…</think> blocks. Only the clean answer is surfaced
	// and stored in history (reasoning is ephemeral; it is dropped, not echoed back
	// to the model). Surfacing it to the UI is a separate, deferred feature.
	answer, inlineReasoning := splitReasoning(content.String())
	if dropped := strings.TrimSpace(reasoning.String() + "\n" + inlineReasoning); dropped != "" {
		log.WithFields(log.Fields{"provider_session_id": s.id, "reasoning_chars": len(dropped)}).
			Debug("openai: separated reasoning from answer")
	}

	toolCalls := tools.finalize()
	if finishReason == "tool_calls" || len(toolCalls) > 0 {
		s.appendHistory(chatMessage{Role: "assistant", Content: answer, ToolCalls: toolCalls})
		ids := make([]string, 0, len(toolCalls))
		for _, tc := range toolCalls {
			s.enqueue(agents.ProviderEvent{
				ProviderEventID: tc.ID,
				Type:            agents.ProviderEventCustomToolUseStarted,
				ToolName:        tc.Function.Name,
				ToolCallID:      tc.ID,
				ToolInput:       tc.Function.Arguments,
				CustomToolUse: &agents.CustomToolUse{
					ID:    tc.ID,
					Name:  tc.Function.Name,
					Input: tc.Function.Arguments,
				},
			})
			ids = append(ids, tc.ID)
		}
		s.enqueue(agents.ProviderEvent{
			Type:               agents.ProviderEventCustomToolResultsRequired,
			CustomToolEventIDs: ids,
		})
		return
	}

	s.appendHistory(chatMessage{Role: "assistant", Content: answer})
	// Skip an empty assistant_message (e.g. a reasoning-only turn with no answer);
	// always emit the terminal event.
	if answer != "" {
		s.enqueue(agents.ProviderEvent{
			ProviderEventID: uuid.NewString(),
			Type:            agents.ProviderEventAssistantMessage,
			Text:            answer,
		})
	}
	s.enqueue(agents.ProviderEvent{Type: agents.ProviderEventTurnCompleted})
}

// SendCustomToolResults appends the tool results to the session history and
// resumes the turn; the continuation may produce more text or further tool
// calls, all drained by the same in-flight StreamEvents call.
func (p *Provider) SendCustomToolResults(_ context.Context, providerSessionID string, results []agents.CustomToolResult) error {
	s, err := p.getSession(providerSessionID)
	if err != nil {
		return err
	}

	msgs := make([]chatMessage, 0, len(results))
	for _, r := range results {
		msgs = append(msgs, chatMessage{
			Role:       "tool",
			ToolCallID: r.CustomToolUseID,
			Content:    r.Content,
		})
	}
	s.appendHistory(msgs...)

	runCtx, cancel := context.WithCancel(context.Background())
	s.setCancel(cancel)
	go p.runTurn(runCtx, s)
	return nil
}

func (p *Provider) InterruptSession(_ context.Context, providerSessionID string) error {
	s, err := p.getSession(providerSessionID)
	if err != nil {
		return err
	}
	s.interrupt()
	return nil
}

// DefineOutcome is unsupported: the rubric-driven autonomous loop is a managed-
// agent capability with no OpenAI-compatible equivalent.
func (p *Provider) DefineOutcome(_ context.Context, _ string, _ agents.DefineOutcomeOptions) error {
	return ErrDefineOutcomeUnsupported
}

// StreamEvents drains the session's event channel until a terminal event, ctx
// cancellation, or the channel closing (DeleteSession).
func (p *Provider) StreamEvents(ctx context.Context, providerSessionID string, onEvent func(agents.ProviderEvent) error) error {
	s, err := p.getSession(providerSessionID)
	if err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event, ok := <-s.events:
			if !ok {
				return nil
			}
			if err := onEvent(event); err != nil {
				return err
			}
			if isTerminal(event.Type) {
				return nil
			}
		}
	}
}

func (p *Provider) DeleteSession(_ context.Context, providerSessionID string) error {
	p.mu.Lock()
	s, ok := p.sessions[providerSessionID]
	if ok {
		delete(p.sessions, providerSessionID)
	}
	p.mu.Unlock()

	if ok {
		s.close()
	}
	return nil
}

func isTerminal(t agents.ProviderEventType) bool {
	return t == agents.ProviderEventTurnCompleted ||
		t == agents.ProviderEventSessionFailed
}
