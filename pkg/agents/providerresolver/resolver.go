// Package providerresolver implements agents.Resolver over per-organization
// agent-provider config. It builds an OpenAI-compatible provider from an
// organization's active row, caches the (stateful — in-memory sessions)
// instances keyed by org + a config fingerprint so a config edit yields a fresh
// instance, and falls back to the installation-wide provider when an org has
// none — the admin-configured OpenAI-compatible endpoint (resolved live from the
// DB, cached the same way) if set, otherwise the environment-built one.
package providerresolver

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sync"

	"github.com/google/uuid"
	"github.com/superplanehq/superplane/pkg/agents"
	"github.com/superplanehq/superplane/pkg/agents/openai"
	"github.com/superplanehq/superplane/pkg/crypto"
	"github.com/superplanehq/superplane/pkg/models"
)

// Resolver builds and caches per-organization agent providers.
type Resolver struct {
	fallback agents.Provider
	enc      crypto.Encryptor
	tools    []openai.ToolDefinition

	// lookup returns the org's active provider row, or (nil, nil) when none.
	// Defaults to the DB query; overridable in tests.
	lookup func(uuid.UUID) (*models.OrganizationAgentProvider, error)

	// installationLookup returns the installation metadata carrying the
	// admin-configured agent provider. Defaults to the DB query; overridable in tests.
	installationLookup func() (*models.InstallationMetadata, error)

	mu    sync.Mutex
	cache map[string]agents.Provider
}

var _ agents.Resolver = (*Resolver)(nil)

// New returns a Resolver. fallback is the installation-wide provider used for
// organizations without their own config (may be nil → those orgs have no agent).
func New(fallback agents.Provider, enc crypto.Encryptor, tools []openai.ToolDefinition) *Resolver {
	return &Resolver{
		fallback:           fallback,
		enc:                enc,
		tools:              tools,
		lookup:             models.FindActiveAgentProviderByOrganization,
		installationLookup: models.GetInstallationMetadata,
		cache:              map[string]agents.Provider{},
	}
}

func (r *Resolver) ProviderForOrganization(ctx context.Context, organizationID uuid.UUID) (agents.Provider, error) {
	row, err := r.lookup(organizationID)
	if err != nil {
		return nil, fmt.Errorf("load org agent provider: %w", err)
	}
	if row == nil {
		return r.installationProvider(ctx)
	}

	key := cacheKey(row)

	r.mu.Lock()
	defer r.mu.Unlock()
	if p, ok := r.cache[key]; ok {
		return p, nil
	}

	apiKey, err := row.DecryptAPIKey(ctx, r.enc)
	if err != nil {
		return nil, fmt.Errorf("decrypt org agent provider key: %w", err)
	}
	provider, err := openai.New(openai.Config{
		BaseURL: row.BaseURL,
		APIKey:  apiKey,
		Model:   row.Model,
		Tools:   r.tools,
	})
	if err != nil {
		return nil, fmt.Errorf("build org agent provider: %w", err)
	}

	r.cache[key] = provider
	return provider, nil
}

// cacheKey is the org plus a fingerprint of the connection config, so editing
// the base URL, model, or rotating the key yields a fresh provider rather than a
// stale cached one. (Orphaned instances are released on process restart.)
func cacheKey(row *models.OrganizationAgentProvider) string {
	fp := sha256.Sum256([]byte(row.BaseURL + "|" + row.Model + "|" + row.APIKeyEnc))
	return fmt.Sprintf("%s|%x", row.OrganizationID, fp[:8])
}

// installationProvider returns the installation-wide provider for organizations
// without their own config: the admin-configured OpenAI-compatible endpoint
// (resolved live from the DB and cached by config fingerprint, so an admin edit
// yields a fresh instance) when set, otherwise the environment-built fallback.
func (r *Resolver) installationProvider(ctx context.Context) (agents.Provider, error) {
	md, err := r.installationLookup()
	if err != nil {
		return nil, fmt.Errorf("load installation agent config: %w", err)
	}
	if md == nil || !md.UsesOpenAIAgent() {
		return r.fallback, nil
	}

	key := installationCacheKey(md)

	r.mu.Lock()
	defer r.mu.Unlock()
	if p, ok := r.cache[key]; ok {
		return p, nil
	}

	apiKey, err := md.DecryptAgentAPIKey(ctx, r.enc)
	if err != nil {
		return nil, fmt.Errorf("decrypt installation agent key: %w", err)
	}
	provider, err := openai.New(openai.Config{
		BaseURL: md.AgentBaseURL,
		APIKey:  apiKey,
		Model:   md.AgentModel,
		Tools:   r.tools,
	})
	if err != nil {
		return nil, fmt.Errorf("build installation agent provider: %w", err)
	}

	r.cache[key] = provider
	return provider, nil
}

// installationCacheKey fingerprints the installation connection config so an
// admin editing the base URL, model, or key yields a fresh provider. Namespaced
// "installation|" so it never collides with the per-org keys (which lead with the
// org UUID).
func installationCacheKey(md *models.InstallationMetadata) string {
	fp := sha256.Sum256([]byte(md.AgentBaseURL + "|" + md.AgentModel + "|" + md.AgentAPIKeyEnc))
	return fmt.Sprintf("installation|%x", fp[:8])
}
