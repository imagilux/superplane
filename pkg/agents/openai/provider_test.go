package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
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
