// Package providerresolver implements agents.Resolver over per-organization
// agent-provider config. It builds an OpenAI-compatible provider from an
// organization's active row, caches the (stateful — in-memory sessions)
// instances keyed by org + a config fingerprint so a config edit yields a fresh
// instance, and falls back to an installation-wide provider when an org has none.
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

	mu    sync.Mutex
	cache map[string]agents.Provider
}

var _ agents.Resolver = (*Resolver)(nil)

// New returns a Resolver. fallback is the installation-wide provider used for
// organizations without their own config (may be nil → those orgs have no agent).
func New(fallback agents.Provider, enc crypto.Encryptor, tools []openai.ToolDefinition) *Resolver {
	return &Resolver{
		fallback: fallback,
		enc:      enc,
		tools:    tools,
		lookup:   models.FindActiveAgentProviderByOrganization,
		cache:    map[string]agents.Provider{},
	}
}

func (r *Resolver) ProviderForOrganization(ctx context.Context, organizationID uuid.UUID) (agents.Provider, error) {
	row, err := r.lookup(organizationID)
	if err != nil {
		return nil, fmt.Errorf("load org agent provider: %w", err)
	}
	if row == nil {
		return r.fallback, nil
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
