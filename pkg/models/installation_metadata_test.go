package models

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/superplanehq/superplane/pkg/crypto"
	"github.com/superplanehq/superplane/pkg/database"
)

func TestGetInstallationMetadata(t *testing.T) {
	require.NoError(t, database.TruncateTables())

	metadata, err := GetInstallationMetadata()
	require.NoError(t, err)
	require.NotNil(t, metadata)
	assert.Equal(t, installationMetadataID, metadata.ID)
	assert.NotEmpty(t, metadata.InstallationID)
	assert.False(t, metadata.AllowPrivateNetworkAccess)
}

func TestUpdateInstallationMetadata(t *testing.T) {
	require.NoError(t, database.TruncateTables())

	metadata, err := GetInstallationMetadata()
	require.NoError(t, err)

	metadata.AllowPrivateNetworkAccess = true
	metadata.UpdatedAt = time.Now()

	require.NoError(t, UpdateInstallationMetadata(metadata))

	updated, err := GetInstallationMetadata()
	require.NoError(t, err)
	assert.True(t, updated.AllowPrivateNetworkAccess)
	assert.Equal(t, metadata.InstallationID, updated.InstallationID)
}

func TestInstallationAgentConfig(t *testing.T) {
	require.NoError(t, database.TruncateTables())
	ctx := context.Background()
	enc := crypto.NewNoOpEncryptor()

	metadata, err := GetInstallationMetadata()
	require.NoError(t, err)
	assert.False(t, metadata.UsesOpenAIAgent())
	assert.False(t, metadata.HasAgentAPIKey())

	metadata.AgentProvider = AgentProviderTypeOpenAI
	metadata.AgentBaseURL = "http://localhost:8080/v1"
	metadata.AgentModel = "qwen3"
	require.NoError(t, metadata.SetAgentAPIKey(ctx, enc, "sk-secret"))
	metadata.UpdatedAt = time.Now()
	require.NoError(t, UpdateInstallationMetadata(metadata))

	updated, err := GetInstallationMetadata()
	require.NoError(t, err)
	assert.Equal(t, "http://localhost:8080/v1", updated.AgentBaseURL)
	assert.Equal(t, "qwen3", updated.AgentModel)
	assert.True(t, updated.UsesOpenAIAgent())
	assert.True(t, updated.HasAgentAPIKey())
	assert.NotEqual(t, "sk-secret", updated.AgentAPIKeyEnc, "the key must be stored encoded, not as plaintext")

	plain, err := updated.DecryptAgentAPIKey(ctx, enc)
	require.NoError(t, err)
	assert.Equal(t, "sk-secret", plain)

	// An empty key clears it.
	require.NoError(t, updated.SetAgentAPIKey(ctx, enc, ""))
	metadata.UpdatedAt = time.Now()
	require.NoError(t, UpdateInstallationMetadata(updated))
	cleared, err := GetInstallationMetadata()
	require.NoError(t, err)
	assert.False(t, cleared.HasAgentAPIKey())
}
