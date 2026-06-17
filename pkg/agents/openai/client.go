package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
)

// chatMessage is one message in the conversation sent to the Chat Completions
// API. Assistant turns may carry tool_calls; tool results use role:"tool" with
// the matching tool_call_id.
type chatMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []toolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
}

// toolCall is an assistant's request to invoke a function tool.
type toolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function toolCallFunction `json:"function"`
}

type toolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// toolSpec advertises a function tool to the model.
type toolSpec struct {
	Type     string           `json:"type"`
	Function toolSpecFunction `json:"function"`
}

type toolSpecFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type chatCompletionRequest struct {
	Model      string        `json:"model"`
	Messages   []chatMessage `json:"messages"`
	Stream     bool          `json:"stream"`
	Tools      []toolSpec    `json:"tools,omitempty"`
	ToolChoice string        `json:"tool_choice,omitempty"`
}

// chatCompletionChunk is one streamed Server-Sent-Events frame from the Chat
// Completions API (the shape after the `data: ` prefix).
type chatCompletionChunk struct {
	Choices []chunkChoice `json:"choices"`
	Error   *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type chunkChoice struct {
	Delta        chunkDelta `json:"delta"`
	FinishReason string     `json:"finish_reason"`
}

type chunkDelta struct {
	Content string `json:"content"`
	// Reasoning models stream chain-of-thought separately from the answer.
	// reasoning_content is the vLLM/DeepSeek convention; reasoning is a variant.
	ReasoningContent string               `json:"reasoning_content"`
	Reasoning        string               `json:"reasoning"`
	ToolCalls        []chunkToolCallDelta `json:"tool_calls"`
}

// chunkToolCallDelta is a streamed fragment of a tool call: the id + name arrive
// in the first fragment for a given index, and arguments arrive split across
// subsequent fragments.
type chunkToolCallDelta struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// streamCompletion POSTs a streaming chat-completion request and invokes onChunk
// for each parsed SSE frame until the `[DONE]` sentinel, ctx cancellation, or
// onChunk returns an error. Malformed frames are skipped.
func (p *Provider) streamCompletion(ctx context.Context, messages []chatMessage, onChunk func(chatCompletionChunk) error) error {
	reqBody := chatCompletionRequest{
		Model:    p.model,
		Messages: messages,
		Stream:   true,
	}
	if len(p.tools) > 0 {
		reqBody.Tools = p.toolSpecs()
		reqBody.ToolChoice = "auto"
	}

	buf, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("openai: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(buf))
	if err != nil {
		return fmt.Errorf("openai: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("openai API %d: %s", resp.StatusCode, truncate(string(body), 500))
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" {
			continue
		}
		if payload == "[DONE]" {
			return nil
		}
		var chunk chatCompletionChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue // tolerate keep-alive comments / malformed frames
		}
		if err := onChunk(chunk); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("openai: stream read: %w", err)
	}
	return nil
}

func (p *Provider) toolSpecs() []toolSpec {
	specs := make([]toolSpec, 0, len(p.tools))
	for _, t := range p.tools {
		specs = append(specs, toolSpec{
			Type: "function",
			Function: toolSpecFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
			},
		})
	}
	return specs
}

// toolCallAccumulator reassembles streamed tool-call fragments (keyed by index)
// into complete tool calls: id + name land in the first fragment, arguments
// arrive split across later fragments.
type toolCallAccumulator struct {
	byIndex map[int]*toolCall
	order   []int
}

func newToolCallAccumulator() *toolCallAccumulator {
	return &toolCallAccumulator{byIndex: map[int]*toolCall{}}
}

func (a *toolCallAccumulator) add(deltas []chunkToolCallDelta) {
	for _, d := range deltas {
		tc, ok := a.byIndex[d.Index]
		if !ok {
			tc = &toolCall{Type: "function"}
			a.byIndex[d.Index] = tc
			a.order = append(a.order, d.Index)
		}
		if d.ID != "" {
			tc.ID = d.ID
		}
		if d.Function.Name != "" {
			tc.Function.Name = d.Function.Name
		}
		tc.Function.Arguments += d.Function.Arguments
	}
}

func (a *toolCallAccumulator) finalize() []toolCall {
	sort.Ints(a.order)
	out := make([]toolCall, 0, len(a.order))
	for _, idx := range a.order {
		out = append(out, *a.byIndex[idx])
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
