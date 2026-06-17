package openai

import (
	"context"
	"sync"

	"github.com/superplanehq/superplane/pkg/agents"
)

// eventBufferSize is generous: final-emit produces only two ProviderEvents per
// turn (assistant_message + a terminal), and StreamEvents drains per turn, so
// the buffer never fills in practice.
const eventBufferSize = 256

// session is the client-side state the provider keeps for one logical agent
// session, since an OpenAI-compatible endpoint is stateless. It holds the running
// conversation history and bridges SendMessage (which runs a completion in a
// goroutine) to StreamEvents (which drains the resulting ProviderEvents) — the
// same channel-per-session pattern as test/support.TestAgentProvider.
type session struct {
	id string

	mu      sync.Mutex
	history []chatMessage
	cancel  context.CancelFunc
	events  chan agents.ProviderEvent
	closed  bool
	// outcome is non-nil while a DefineOutcome autonomous loop is running.
	outcome *outcomeState
}

// outcomeState tracks an in-flight DefineOutcome loop: the rubric to grade
// against, the iteration cap, the current 0-indexed iteration, and the context
// preamble re-injected on each revision.
type outcomeState struct {
	rubric        string
	maxIterations int
	iteration     int
	preamble      string
}

func newSession(id string, history []chatMessage) *session {
	return &session{
		id:      id,
		history: history,
		events:  make(chan agents.ProviderEvent, eventBufferSize),
	}
}

func (s *session) snapshotHistory() []chatMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]chatMessage(nil), s.history...)
}

func (s *session) appendHistory(msgs ...chatMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.history = append(s.history, msgs...)
}

func (s *session) startOutcome(oc *outcomeState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.outcome = oc
}

func (s *session) currentOutcome() *outcomeState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.outcome
}

func (s *session) clearOutcome() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.outcome = nil
}

func (s *session) setCancel(cancel context.CancelFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancel = cancel
}

func (s *session) interrupt() {
	s.mu.Lock()
	cancel := s.cancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// enqueue is non-blocking and guarded so it never sends on a closed channel and
// never blocks the completion goroutine.
func (s *session) enqueue(event agents.ProviderEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	select {
	case s.events <- event:
	default:
	}
}

func (s *session) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	if s.cancel != nil {
		s.cancel()
	}
	close(s.events)
}
