package sso

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClaimStrings(t *testing.T) {
	assert.Equal(t, []string{"a", "b"}, claimStrings(map[string]any{"g": []any{"a", "b"}}, "g"))
	assert.Equal(t, []string{"solo"}, claimStrings(map[string]any{"g": "solo"}, "g"),
		"a single string becomes a one-element slice")
	assert.Equal(t, []string{"x"}, claimStrings(map[string]any{"g": []any{"x", 42, nil}}, "g"),
		"non-string elements are skipped")
	assert.Nil(t, claimStrings(map[string]any{}, "g"), "absent claim is nil")
	assert.Nil(t, claimStrings(map[string]any{"g": ""}, "g"), "empty string is nil")
}

func TestClaimStringAndBool(t *testing.T) {
	m := map[string]any{"s": "v", "b": true, "n": 1}
	assert.Equal(t, "v", claimString(m, "s"))
	assert.Equal(t, "", claimString(m, "missing"))
	assert.Equal(t, "", claimString(m, "n"), "a non-string value reads as empty")
	assert.True(t, claimBool(m, "b"))
	assert.False(t, claimBool(m, "missing"))
	assert.False(t, claimBool(m, "s"), "a non-bool value reads as false")
}
