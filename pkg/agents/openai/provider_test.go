package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/superplanehq/superplane/pkg/agents"
)

// streamingServer returns a mock OpenAI-compatible endpoint that streams the
// given content deltas, then a finish_reason:stop frame, then [DONE]. assertReq
// (optional) inspects the inbound request.
func streamingServer(t *testing.T, deltas []string, assertReq func(r *http.Request, body []byte)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if assertReq != nil {
			assertReq(r, body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		for _, d := range deltas {
			fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q},\"finish_reason\":null}]}\n\n", d)
			if flusher != nil {
				flusher.Flush()
			}
		}
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	}))
}

func collect(t *testing.T, p *Provider, sid string) []agents.ProviderEvent {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var events []agents.ProviderEvent
	err := p.StreamEvents(ctx, sid, func(e agents.ProviderEvent) error {
		events = append(events, e)
		return nil
	})
	require.NoError(t, err)
	return events
}

func TestProviderStreamsAssistantMessage(t *testing.T) {
	var gotAuth string
	var gotBody map[string]any
	srv := streamingServer(t, []string{"Hello", ", ", "world"}, func(r *http.Request, body []byte) {
		gotAuth = r.Header.Get("Authorization")
		_ = json.Unmarshal(body, &gotBody)
	})
	defer srv.Close()

	p, err := New(Config{BaseURL: srv.URL, APIKey: "sk-test", Model: "test-model", HTTPClient: srv.Client()})
	require.NoError(t, err)

	res, err := p.CreateSession(context.Background(), agents.CreateSessionOptions{})
	require.NoError(t, err)
	sid := res.ProviderSessionID

	require.NoError(t, p.SendMessage(context.Background(), sid, "hi there", agents.SendMessageOptions{ContextPreamble: "PREAMBLE"}))

	events := collect(t, p, sid)
	require.Len(t, events, 2)
	assert.Equal(t, agents.ProviderEventAssistantMessage, events[0].Type)
	assert.Equal(t, "Hello, world", events[0].Text)
	assert.NotEmpty(t, events[0].ProviderEventID)
	assert.Equal(t, agents.ProviderEventTurnCompleted, events[1].Type)

	// Auth header + request body.
	assert.Equal(t, "Bearer sk-test", gotAuth)
	assert.Equal(t, "test-model", gotBody["model"])
	assert.Equal(t, true, gotBody["stream"])
	_, hasTools := gotBody["tools"]
	assert.False(t, hasTools, "no tools array when none configured")
	msgs, ok := gotBody["messages"].([]any)
	require.True(t, ok)
	last := msgs[len(msgs)-1].(map[string]any)
	assert.Equal(t, "user", last["role"])
	content := last["content"].(string)
	assert.Contains(t, content, "PREAMBLE")
	assert.Contains(t, content, "hi there")

	// History grew: system + user + assistant (at least 3).
	p.mu.Lock()
	s := p.sessions[sid]
	p.mu.Unlock()
	require.NotNil(t, s)
	assert.GreaterOrEqual(t, len(s.snapshotHistory()), 3)
}

func TestProviderStreamErrorYieldsSessionFailed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "boom")
	}))
	defer srv.Close()

	p, err := New(Config{BaseURL: srv.URL, Model: "m", HTTPClient: srv.Client()})
	require.NoError(t, err)
	res, err := p.CreateSession(context.Background(), agents.CreateSessionOptions{})
	require.NoError(t, err)
	require.NoError(t, p.SendMessage(context.Background(), res.ProviderSessionID, "hi", agents.SendMessageOptions{}))

	events := collect(t, p, res.ProviderSessionID)
	require.Len(t, events, 1)
	assert.Equal(t, agents.ProviderEventSessionFailed, events[0].Type)
	assert.Contains(t, events[0].ErrorMessage, "500")
}

func TestProviderDefaultClientGuardsAgainstSSRF(t *testing.T) {
	// With no injected client, the provider uses the SSRF-guarded default. A
	// link-local target (here the cloud-metadata address) must be refused at
	// dial time, surfacing as a failed session rather than a reachable fetch.
	p, err := New(Config{BaseURL: "http://169.254.169.254/v1", Model: "m"})
	require.NoError(t, err)
	res, err := p.CreateSession(context.Background(), agents.CreateSessionOptions{})
	require.NoError(t, err)
	require.NoError(t, p.SendMessage(context.Background(), res.ProviderSessionID, "hi", agents.SendMessageOptions{}))

	events := collect(t, p, res.ProviderSessionID)
	require.Len(t, events, 1)
	assert.Equal(t, agents.ProviderEventSessionFailed, events[0].Type)
	assert.Contains(t, events[0].ErrorMessage, "blocked address")
}

func TestProviderDefineOutcomeUnsupported(t *testing.T) {
	p, err := New(Config{BaseURL: "http://example.invalid", Model: "m"})
	require.NoError(t, err)
	res, err := p.CreateSession(context.Background(), agents.CreateSessionOptions{})
	require.NoError(t, err)

	err = p.DefineOutcome(context.Background(), res.ProviderSessionID, agents.DefineOutcomeOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}

func TestProviderNoAPIKeyOmitsAuthHeader(t *testing.T) {
	var hadAuth bool
	srv := streamingServer(t, []string{"ok"}, func(r *http.Request, _ []byte) {
		_, hadAuth = r.Header["Authorization"]
	})
	defer srv.Close()

	p, err := New(Config{BaseURL: srv.URL, Model: "m", HTTPClient: srv.Client()}) // no APIKey
	require.NoError(t, err)
	res, err := p.CreateSession(context.Background(), agents.CreateSessionOptions{})
	require.NoError(t, err)
	require.NoError(t, p.SendMessage(context.Background(), res.ProviderSessionID, "hi", agents.SendMessageOptions{}))

	collect(t, p, res.ProviderSessionID)
	assert.False(t, hadAuth, "Authorization header must be absent when no API key is set")
}

func TestProviderMissingSessionIsRecoverable(t *testing.T) {
	p, err := New(Config{BaseURL: "http://example.invalid", Model: "m"})
	require.NoError(t, err)

	err = p.SendMessage(context.Background(), "does-not-exist", "hi", agents.SendMessageOptions{})
	require.Error(t, err)
	assert.ErrorIs(t, err, agents.ErrProviderSessionUnavailable)
}

// collectUntilToolResults drains events up to and including the first
// custom_tool_results_required (which is NOT terminal), mirroring the worker,
// which stops StreamEvents there to go execute the tools.
func collectUntilToolResults(t *testing.T, p *Provider, sid string) []agents.ProviderEvent {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stop := errors.New("stop")
	var events []agents.ProviderEvent
	err := p.StreamEvents(ctx, sid, func(e agents.ProviderEvent) error {
		events = append(events, e)
		if e.Type == agents.ProviderEventCustomToolResultsRequired {
			return stop
		}
		return nil
	})
	if err != nil && !errors.Is(err, stop) {
		require.NoError(t, err)
	}
	return events
}

func TestProviderToolCallRoundTrip(t *testing.T) {
	var mu sync.Mutex
	var bodies [][]byte
	reqN := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, body)
		n := reqN
		reqN++
		mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		if n == 0 {
			// One tool call, fragmented across two frames to exercise accumulation.
			fmt.Fprint(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"update_draft","arguments":"{\"x\":"}}]},"finish_reason":null}]}`+"\n\n")
			if flusher != nil {
				flusher.Flush()
			}
			fmt.Fprint(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"1}"}}]},"finish_reason":null}]}`+"\n\n")
			fmt.Fprint(w, `data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`+"\n\n")
			fmt.Fprint(w, "data: [DONE]\n\n")
		} else {
			fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"all done"},"finish_reason":null}]}`+"\n\n")
			fmt.Fprint(w, `data: {"choices":[{"delta":{},"finish_reason":"stop"}]}`+"\n\n")
			fmt.Fprint(w, "data: [DONE]\n\n")
		}
		if flusher != nil {
			flusher.Flush()
		}
	}))
	defer srv.Close()

	p, err := New(Config{
		BaseURL:    srv.URL,
		Model:      "m",
		HTTPClient: srv.Client(),
		Tools:      []ToolDefinition{{Name: "update_draft", Description: "edit the draft", Parameters: map[string]any{"type": "object"}}},
	})
	require.NoError(t, err)

	res, err := p.CreateSession(context.Background(), agents.CreateSessionOptions{})
	require.NoError(t, err)
	sid := res.ProviderSessionID

	require.NoError(t, p.SendMessage(context.Background(), sid, "build it", agents.SendMessageOptions{}))

	ev1 := collectUntilToolResults(t, p, sid)
	require.Len(t, ev1, 2)
	assert.Equal(t, agents.ProviderEventCustomToolUseStarted, ev1[0].Type)
	assert.Equal(t, "update_draft", ev1[0].ToolName)
	require.NotNil(t, ev1[0].CustomToolUse)
	assert.Equal(t, "call_1", ev1[0].CustomToolUse.ID)
	assert.JSONEq(t, `{"x":1}`, ev1[0].CustomToolUse.Input)
	assert.Equal(t, agents.ProviderEventCustomToolResultsRequired, ev1[1].Type)
	assert.Equal(t, []string{"call_1"}, ev1[1].CustomToolEventIDs)

	require.NoError(t, p.SendCustomToolResults(context.Background(), sid, []agents.CustomToolResult{
		{CustomToolUseID: "call_1", Content: `{"ok":true}`},
	}))

	ev2 := collect(t, p, sid)
	require.Len(t, ev2, 2)
	assert.Equal(t, agents.ProviderEventAssistantMessage, ev2[0].Type)
	assert.Equal(t, "all done", ev2[0].Text)
	assert.Equal(t, agents.ProviderEventTurnCompleted, ev2[1].Type)

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, bodies, 2)

	// Both requests advertise the tool.
	for i, b := range bodies {
		var req map[string]any
		require.NoError(t, json.Unmarshal(b, &req))
		tools, ok := req["tools"].([]any)
		require.Truef(t, ok && len(tools) > 0, "request %d missing tools", i)
		fn := tools[0].(map[string]any)["function"].(map[string]any)
		assert.Equal(t, "update_draft", fn["name"])
	}

	// The second request carries the assistant tool_calls message and the tool result.
	var req2 map[string]any
	require.NoError(t, json.Unmarshal(bodies[1], &req2))
	msgs := req2["messages"].([]any)
	var sawAssistantToolCalls, sawToolResult bool
	for _, m := range msgs {
		msg := m.(map[string]any)
		if msg["role"] == "assistant" {
			if tc, ok := msg["tool_calls"].([]any); ok && len(tc) > 0 {
				sawAssistantToolCalls = true
			}
		}
		if msg["role"] == "tool" && msg["tool_call_id"] == "call_1" {
			sawToolResult = true
		}
	}
	assert.True(t, sawAssistantToolCalls, "assistant message with tool_calls must precede tool results")
	assert.True(t, sawToolResult, "tool result message with matching tool_call_id")
}

func TestSplitReasoning(t *testing.T) {
	cases := []struct{ name, in, answer, reasoning string }{
		{"plain", "plain answer", "plain answer", ""},
		{"leading think", "<think>weigh options</think>final answer", "final answer", "weigh options"},
		{"mid think", "a<think>mid</think>b", "ab", "mid"},
		{"unterminated", "<think>only reasoning, no answer", "", "only reasoning, no answer"},
		{"answer then open think", "answer then <think>late stuff", "answer then", "late stuff"},
	}
	for _, c := range cases {
		a, r := splitReasoning(c.in)
		assert.Equalf(t, c.answer, a, "%s: answer", c.name)
		assert.Equalf(t, c.reasoning, r, "%s: reasoning", c.name)
	}
}

func TestProviderSeparatesReasoning(t *testing.T) {
	// One frame carries reasoning_content, the next an answer with an inline
	// <think> block; both reasoning sources must be stripped from what is surfaced.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"choices":[{"delta":{"reasoning_content":"let me think"},"finish_reason":null}]}`+"\n\n")
		fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"<think>and more</think>the answer"},"finish_reason":null}]}`+"\n\n")
		fmt.Fprint(w, `data: {"choices":[{"delta":{},"finish_reason":"stop"}]}`+"\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}))
	defer srv.Close()

	p, err := New(Config{BaseURL: srv.URL, Model: "m", HTTPClient: srv.Client()})
	require.NoError(t, err)
	res, err := p.CreateSession(context.Background(), agents.CreateSessionOptions{})
	require.NoError(t, err)
	require.NoError(t, p.SendMessage(context.Background(), res.ProviderSessionID, "q", agents.SendMessageOptions{}))

	events := collect(t, p, res.ProviderSessionID)
	require.Len(t, events, 2)
	assert.Equal(t, agents.ProviderEventAssistantMessage, events[0].Type)
	assert.Equal(t, "the answer", events[0].Text)
	assert.Equal(t, agents.ProviderEventTurnCompleted, events[1].Type)
}
