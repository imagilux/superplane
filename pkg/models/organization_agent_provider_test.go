package models

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/superplanehq/superplane/pkg/crypto"
)

// DB-free: exercises the API-key encryption round-trip + optional-key handling
// directly on an in-memory provider (no database, no Postgres needed).
func TestOrganizationAgentProvider_APIKeyRoundTrip(t *testing.T) {
	ctx := context.Background()
	enc := crypto.NewAESGCMEncryptor([]byte("1234567890abcdefghijklmnopqrstuv"))

	p := NewAgentProvider(uuid.New(), nil, "local-llm", "Local LLM", "", "http://localhost:8000/v1", "qwen2.5-coder", true)
	assert.Equal(t, AgentProviderTypeOpenAI, p.Type, "type defaults to openai")
	assert.False(t, p.HasAPIKey(), "no key by default")

	// A non-empty key encrypts, never stores plaintext, and round-trips.
	require.NoError(t, p.SetAPIKey(ctx, enc, "sk-secret-123"))
	assert.True(t, p.HasAPIKey())
	assert.NotEmpty(t, p.APIKeyEnc)
	assert.NotContains(t, p.APIKeyEnc, "sk-secret-123")

	got, err := p.DecryptAPIKey(ctx, enc)
	require.NoError(t, err)
	assert.Equal(t, "sk-secret-123", got)

	// An empty key clears it; decrypt returns empty, no error (unauthenticated servers).
	require.NoError(t, p.SetAPIKey(ctx, enc, ""))
	assert.False(t, p.HasAPIKey())
	assert.Empty(t, p.APIKeyEnc)

	got, err = p.DecryptAPIKey(ctx, enc)
	require.NoError(t, err)
	assert.Empty(t, got)
}
