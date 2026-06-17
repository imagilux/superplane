package openai

import (
	"regexp"
	"strings"
)

// thinkBlock matches a complete inline <think>…</think> reasoning block
// (case-insensitive, dotall). Reasoning models without a server-side reasoning
// parser emit chain-of-thought this way, inside the regular content.
var thinkBlock = regexp.MustCompile(`(?is)<think>(.*?)</think>`)

// openThink matches an opening <think> tag, used to salvage an unterminated block.
var openThink = regexp.MustCompile(`(?i)<think>`)

// splitReasoning separates inline <think> reasoning from the answer in a complete
// (final-emit) content string, returning the clean answer and the extracted
// reasoning. An unterminated <think> (no closing tag) is treated as reasoning
// through the end of the string. This complements the dedicated reasoning_content
// delta field, which the caller accumulates separately.
func splitReasoning(content string) (answer, reasoning string) {
	var rb strings.Builder
	for _, m := range thinkBlock.FindAllStringSubmatch(content, -1) {
		rb.WriteString(m[1])
	}
	answer = thinkBlock.ReplaceAllString(content, "")

	if loc := openThink.FindStringIndex(answer); loc != nil {
		rb.WriteString(answer[loc[1]:])
		answer = answer[:loc[0]]
	}

	return strings.TrimSpace(answer), strings.TrimSpace(rb.String())
}
