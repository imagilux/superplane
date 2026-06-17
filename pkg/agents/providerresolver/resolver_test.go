package providerresolver

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/superplanehq/superplane/pkg/agents/openai"
	"github.com/superplanehq/superplane/pkg/crypto"
	"github.com/superplanehq/superplane/pkg/models"
)

func TestResolverFallsBackWhenNoRow(t *testing.T) {
	fallback, err := openai.New(openai.Config{BaseURL: "http://fallback/v1", Model: "fb"})
	require.NoError(t, err)

	r := New(fallback, crypto.NewNoOpEncryptor(), nil)
	r.lookup = func(uuid.UUID) (*models.OrganizationAgentProvider, error) { return nil, nil }

	p, err := r.ProviderForOrganization(context.Background(), uuid.New())
	require.NoError(t, err)
	assert.Same(t, fallback, p, "org with no row gets the installation fallback")
}

func TestResolverBuildsAndCachesPerOrg(t *testing.T) {
	org := uuid.New()
	row := models.NewAgentProvider(org, nil, "local", "Local", "openai", "http://localhost:8000/v1", "m", true)

	r := New(nil, crypto.NewNoOpEncryptor(), nil)
	r.lookup = func(uuid.UUID) (*models.OrganizationAgentProvider, error) { return row, nil }

	p1, err := r.ProviderForOrganization(context.Background(), org)
	require.NoError(t, err)
	require.NotNil(t, p1)
	assert.Equal(t, "openai", p1.Name())

	p2, err := r.ProviderForOrganization(context.Background(), org)
	require.NoError(t, err)
	assert.Same(t, p1, p2, "same config returns the same cached (stateful) instance")
}

func TestResolverRebuildsOnConfigChange(t *testing.T) {
	org := uuid.New()
	r := New(nil, crypto.NewNoOpEncryptor(), nil)

	r.lookup = func(uuid.UUID) (*models.OrganizationAgentProvider, error) {
		return models.NewAgentProvider(org, nil, "local", "Local", "openai", "http://a/v1", "m", true), nil
	}
	p1, err := r.ProviderForOrganization(context.Background(), org)
	require.NoError(t, err)

	r.lookup = func(uuid.UUID) (*models.OrganizationAgentProvider, error) {
		return models.NewAgentProvider(org, nil, "local", "Local", "openai", "http://b/v1", "m", true), nil
	}
	p2, err := r.ProviderForOrganization(context.Background(), org)
	require.NoError(t, err)
	assert.NotSame(t, p1, p2, "a changed config builds a fresh instance")
}
