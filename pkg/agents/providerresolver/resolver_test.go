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
	r.installationLookup = func() (*models.InstallationMetadata, error) { return nil, nil }

	p, err := r.ProviderForOrganization(context.Background(), uuid.New())
	require.NoError(t, err)
	assert.Same(t, fallback, p, "org with no row and no DB installation config gets the env fallback")
}

func TestResolverUsesInstallationDBProvider(t *testing.T) {
	// No env fallback and no per-org row, but an admin-configured installation
	// OpenAI provider in the DB → the resolver builds and caches it live.
	r := New(nil, crypto.NewNoOpEncryptor(), nil)
	r.lookup = func(uuid.UUID) (*models.OrganizationAgentProvider, error) { return nil, nil }
	r.installationLookup = func() (*models.InstallationMetadata, error) {
		return &models.InstallationMetadata{
			AgentProvider: "openai",
			AgentBaseURL:  "http://installation/v1",
			AgentModel:    "m",
		}, nil
	}

	p1, err := r.ProviderForOrganization(context.Background(), uuid.New())
	require.NoError(t, err)
	require.NotNil(t, p1)
	assert.Equal(t, "openai", p1.Name())

	p2, err := r.ProviderForOrganization(context.Background(), uuid.New())
	require.NoError(t, err)
	assert.Same(t, p1, p2, "the same installation config returns the cached instance")
}

func TestResolverInstallationFallsBackToEnvWhenNotOpenAI(t *testing.T) {
	fallback, err := openai.New(openai.Config{BaseURL: "http://env-fallback/v1", Model: "fb"})
	require.NoError(t, err)

	r := New(fallback, crypto.NewNoOpEncryptor(), nil)
	r.lookup = func(uuid.UUID) (*models.OrganizationAgentProvider, error) { return nil, nil }
	r.installationLookup = func() (*models.InstallationMetadata, error) {
		return &models.InstallationMetadata{AgentProvider: "anthropic"}, nil
	}

	p, err := r.ProviderForOrganization(context.Background(), uuid.New())
	require.NoError(t, err)
	assert.Same(t, fallback, p, "a non-OpenAI installation config uses the env fallback")
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
