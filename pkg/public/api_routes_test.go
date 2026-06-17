package public

import (
	"testing"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/superplanehq/superplane/pkg/authorization"
	"github.com/superplanehq/superplane/pkg/crypto"
	"github.com/superplanehq/superplane/pkg/git/inmemory"
	"github.com/superplanehq/superplane/pkg/jwt"
	"github.com/superplanehq/superplane/pkg/registry"
	"github.com/superplanehq/superplane/test/support"
)

// TestGRPCGatewayRoutesAreRegistered guards against a gRPC service being
// registered on the gRPC server but missing its public REST gateway wiring in
// RegisterGRPCGateway — the Register*HandlerFromEndpoint call and the matching
// router PathPrefix. When that wiring is missing the service has no HTTP route
// at all: requests fall through to the SPA catch-all instead of reaching the
// gateway. That is exactly how /api/v1/agent-providers shipped dead in
// v0.27.0-rc3 (create silently 404'd, the list returned SPA HTML), and the gRPC
// action unit tests didn't catch it because they call the handlers directly.
//
// Walking the router (rather than issuing requests) keeps the check independent
// of a running gRPC backend: it asserts each known API surface is registered as
// a route. Adding a service without its gateway PathPrefix fails this test.
func TestGRPCGatewayRoutesAreRegistered(t *testing.T) {
	authService, err := authorization.NewAuthService()
	require.NoError(t, err)
	reg, err := registry.NewRegistry(&crypto.NoOpEncryptor{}, registry.HTTPOptions{})
	require.NoError(t, err)
	signer := jwt.NewSigner("test")
	oidcProvider := support.NewOIDCProvider()
	gitProvider := inmemory.NewProvider()

	server, err := NewServer(&crypto.NoOpEncryptor{}, reg, signer, oidcProvider, gitProvider, "", "", "", "test", "/app/templates", authService, nil, false)
	require.NoError(t, err)

	// A dummy address is fine: gRPC dials lazily, so registration itself does
	// not connect — we only inspect which routes got registered.
	require.NoError(t, server.RegisterGRPCGateway("localhost:50051"))

	registered := map[string]bool{}
	require.NoError(t, server.Router.Walk(func(route *mux.Route, _ *mux.Router, _ []*mux.Route) error {
		if tmpl, terr := route.GetPathTemplate(); terr == nil {
			registered[tmpl] = true
		}
		return nil
	}))

	prefixes := []string{
		"/api/v1/users",
		"/api/v1/groups",
		"/api/v1/roles",
		"/api/v1/canvases",
		"/api/v1/canvas-folders",
		"/api/v1/organizations",
		"/api/v1/integrations",
		"/api/v1/secrets",
		"/api/v1/me",
		"/api/v1/actions",
		"/api/v1/triggers",
		"/api/v1/widgets",
		"/api/v1/service-accounts",
		"/api/v1/oidc-providers",
		"/api/v1/agent-providers",
		"/api/v1/agents",
		"/api/v1/workflows",
	}

	for _, prefix := range prefixes {
		assert.True(t, registered[prefix],
			"%s is not registered in RegisterGRPCGateway; its gRPC service is unreachable over HTTP (missing Register*HandlerFromEndpoint and/or router PathPrefix)", prefix)
	}
}
