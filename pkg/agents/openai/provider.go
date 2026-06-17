// Package openai implements agents.Provider against an OpenAI-compatible Chat
// Completions API — a hosted service or a local server such as vLLM or
// llama.cpp. Unlike Anthropic's managed agents, the endpoint is stateless, so
// this provider synthesizes sessions and the agent loop client-side: each
// session keeps its own conversation history, and SendMessage runs the
// completion in a goroutine whose events StreamEvents drains.
//
// Tool calls are surfaced as custom-tool events; the worker executes them and
// calls SendCustomToolResults, which resumes the turn. DefineOutcome runs an
// autonomous build→grade→iterate loop client-side, the model grading its own
// work against the rubric.
package openai

import (
	"context"
	"encoding/json"
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
	history := []chatMessage{{Role: "system", Content: agents.AgentSystemPrompt()}}
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

	err := p.streamCompletion(ctx, s.snapshotHistory(), true, func(chunk chatCompletionChunk) error {
		if chunk.Error != nil && chunk.Error.Message != "" {
			return errors.New(chunk.Error.Message)
		}
		for _, choice := range chunk.Choices {
			content.WriteString(choice.Delta.Content)
			reasoning.WriteString(choice.Delta.ReasoningContent)
			reasoning.WriteString(choice.Delta.Reasoning)
			tools.add(choice.Delta.ToolCalls)
		}
		return nil
	})

	if err != nil {
		// An interrupt (cancelled ctx) is a clean stop, not a failure.
		if errors.Is(err, context.Canceled) {
			if oc := s.currentOutcome(); oc != nil {
				s.enqueue(agents.ProviderEvent{Type: agents.ProviderEventOutcomeEvaluation, OutcomeResult: &agents.OutcomeEvaluation{Iteration: oc.iteration, Result: "interrupted"}})
				s.clearOutcome()
			}
			s.enqueue(agents.ProviderEvent{Type: agents.ProviderEventTurnCompleted})
			return
		}
		s.clearOutcome()
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
	if len(toolCalls) > 0 {
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

	// A reasoning-only turn can leave an empty answer. Do NOT record an empty
	// assistant message: with no content and no tool_calls it serializes to
	// {"role":"assistant"}, which strict servers (llama.cpp) reject on the next
	// request ("Assistant message must contain either 'content' or 'tool_calls'").
	// Skip both the history entry and the assistant_message event; still close the turn.
	if answer != "" {
		s.appendHistory(chatMessage{Role: "assistant", Content: answer})
		s.enqueue(agents.ProviderEvent{
			ProviderEventID: uuid.NewString(),
			Type:            agents.ProviderEventAssistantMessage,
			Text:            answer,
		})
	}
	// In an autonomous outcome loop, grade this iteration's result against the
	// rubric and either finish or revise — rather than completing the turn. No
	// terminal event mid-loop keeps the worker's stream open across iterations.
	if oc := s.currentOutcome(); oc != nil {
		p.evaluateOutcome(ctx, s, oc)
		return
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

// DefineOutcome runs an autonomous build→grade→iterate loop client-side. The
// agent attempts the goal; a separate, tool-less completion then judges the work
// against the rubric and returns a structured pass/fail + explanation; on a fail
// the agent revises with that feedback, up to MaxIterations. It emits the
// outcome_evaluation_start / outcome_evaluation events the worker and UI consume.
// Unlike Anthropic's managed grader, the model grades its own work — faithful,
// but only as reliable as the configured model.
func (p *Provider) DefineOutcome(_ context.Context, providerSessionID string, opts agents.DefineOutcomeOptions) error {
	s, err := p.getSession(providerSessionID)
	if err != nil {
		return err
	}

	maxIter := opts.MaxIterations
	if maxIter <= 0 {
		maxIter = 3
	}
	s.startOutcome(&outcomeState{rubric: opts.Rubric, maxIterations: maxIter, preamble: opts.ContextPreamble})

	goal := opts.Description
	if opts.ContextPreamble != "" {
		goal = opts.ContextPreamble + "\n\n" + opts.Description
	}
	s.appendHistory(chatMessage{Role: "user", Content: goal})
	s.enqueue(agents.ProviderEvent{
		Type:          agents.ProviderEventOutcomeEvaluationStart,
		OutcomeResult: &agents.OutcomeEvaluation{Iteration: 0},
	})

	runCtx, cancel := context.WithCancel(context.Background())
	s.setCancel(cancel)
	go p.runTurn(runCtx, s)
	return nil
}

// evaluateOutcome grades the just-finished build iteration and either completes
// the turn (satisfied, or out of iterations) or revises and runs the next one.
// It runs inside the iteration's runTurn goroutine and tail-spawns the next, so
// only one runTurn touches the session at a time.
func (p *Provider) evaluateOutcome(ctx context.Context, s *session, oc *outcomeState) {
	satisfied, explanation, err := p.gradeAgainstRubric(ctx, s, oc.rubric)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			s.enqueue(agents.ProviderEvent{Type: agents.ProviderEventOutcomeEvaluation, OutcomeResult: &agents.OutcomeEvaluation{Iteration: oc.iteration, Result: "interrupted"}})
			s.enqueue(agents.ProviderEvent{Type: agents.ProviderEventTurnCompleted})
			s.clearOutcome()
			return
		}
		s.clearOutcome()
		s.enqueue(agents.ProviderEvent{Type: agents.ProviderEventSessionFailed, ErrorMessage: fmt.Sprintf("openai: outcome grading failed: %v", err)})
		return
	}

	last := oc.iteration+1 >= oc.maxIterations
	result := "needs_revision"
	switch {
	case satisfied:
		result = "satisfied"
	case last:
		result = "max_iterations_reached"
	}
	s.enqueue(agents.ProviderEvent{
		Type:          agents.ProviderEventOutcomeEvaluation,
		OutcomeResult: &agents.OutcomeEvaluation{Iteration: oc.iteration, Result: result, Explanation: explanation},
	})

	if satisfied || last {
		s.enqueue(agents.ProviderEvent{Type: agents.ProviderEventTurnCompleted})
		s.clearOutcome()
		return
	}

	// Revise: feed the grader's explanation back and run the next iteration.
	oc.iteration++
	revision := fmt.Sprintf(
		"The previous attempt did not satisfy the rubric.\n\nFeedback:\n%s\n\nRevise your work to satisfy this rubric:\n%s",
		explanation, oc.rubric,
	)
	if oc.preamble != "" {
		revision = oc.preamble + "\n\n" + revision
	}
	s.appendHistory(chatMessage{Role: "user", Content: revision})
	s.enqueue(agents.ProviderEvent{
		Type:          agents.ProviderEventOutcomeEvaluationStart,
		OutcomeResult: &agents.OutcomeEvaluation{Iteration: oc.iteration},
	})
	go p.runTurn(ctx, s)
}

// gradeAgainstRubric runs a tool-less completion that judges the conversation's
// work against the rubric, returning a structured pass/fail + explanation. An
// unparseable verdict is treated as not-satisfied so the loop never declares a
// false pass.
func (p *Provider) gradeAgainstRubric(ctx context.Context, s *session, rubric string) (bool, string, error) {
	prompt := "Grade the work done so far in this conversation against the rubric below. " +
		"Do not do any further work or call any tools. Respond with ONLY a JSON object: " +
		`{"satisfied": <true|false>, "explanation": "<one or two sentences>"}.` +
		"\n\nRubric:\n" + rubric
	history := append(s.snapshotHistory(), chatMessage{Role: "user", Content: prompt})

	var content strings.Builder
	err := p.streamCompletion(ctx, history, false, func(chunk chatCompletionChunk) error {
		if chunk.Error != nil && chunk.Error.Message != "" {
			return errors.New(chunk.Error.Message)
		}
		for _, choice := range chunk.Choices {
			content.WriteString(choice.Delta.Content)
		}
		return nil
	})
	if err != nil {
		return false, "", err
	}

	answer, _ := splitReasoning(content.String())
	return parseGradeVerdict(answer)
}

type gradeVerdict struct {
	Satisfied   bool   `json:"satisfied"`
	Explanation string `json:"explanation"`
}

// parseGradeVerdict extracts the {"satisfied":...,"explanation":...} object from
// the grader's reply, tolerating prose or code fences around it. A missing or
// invalid verdict is reported as not-satisfied (never a false pass), surfacing
// the raw text as the explanation.
func parseGradeVerdict(text string) (bool, string, error) {
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return false, strings.TrimSpace(text), nil
	}
	var v gradeVerdict
	if err := json.Unmarshal([]byte(text[start:end+1]), &v); err != nil {
		return false, strings.TrimSpace(text), nil
	}
	return v.Satisfied, strings.TrimSpace(v.Explanation), nil
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
