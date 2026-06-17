package agents

import (
	"context"

	"github.com/google/uuid"
)

// Resolver returns the agents.Provider an organization's agent sessions should
// use. Implementations may select a per-organization provider and fall back to
// an installation-wide default. A (nil, nil) return means no provider is
// available for the organization.
type Resolver interface {
	ProviderForOrganization(ctx context.Context, organizationID uuid.UUID) (Provider, error)
}

// staticResolver returns the same provider for every organization — the
// installation-wide singleton. A nil provider yields (nil, nil).
type staticResolver struct{ provider Provider }

// StaticResolver adapts a single Provider to the Resolver interface, so callers
// (and tests) that hold one provider keep working unchanged.
func StaticResolver(p Provider) Resolver { return staticResolver{provider: p} }

func (r staticResolver) ProviderForOrganization(context.Context, uuid.UUID) (Provider, error) {
	return r.provider, nil
}
