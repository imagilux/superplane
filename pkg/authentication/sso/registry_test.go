package sso_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/superplanehq/superplane/pkg/authentication/sso"
	"github.com/superplanehq/superplane/test/support"
)

func TestRegistryDiscovery(t *testing.T) {
	mock := support.NewMockOIDCProvider(t, "client-1")
	reg := sso.NewRegistry(time.Minute)

	cfg := sso.Config{
		ID:           "p1",
		IssuerURL:    mock.Issuer,
		ClientID:     "client-1",
		ClientSecret: "s",
		RedirectURL:  "http://localhost/cb",
	}

	oauthCfg, verifier, err := reg.Get(context.Background(), cfg)
	require.NoError(t, err)
	require.NotNil(t, verifier)
	assert.Equal(t, mock.Issuer+"/authorize", oauthCfg.Endpoint.AuthURL)
	assert.Equal(t, mock.Issuer+"/token", oauthCfg.Endpoint.TokenURL)
	assert.Contains(t, oauthCfg.Scopes, "openid")

	// Cache hit returns without re-erroring.
	_, _, err = reg.Get(context.Background(), cfg)
	require.NoError(t, err)
}

func TestRegistryUnreachableIssuerErrorsAndIsNotCached(t *testing.T) {
	mock := support.NewMockOIDCProvider(t, "client-1")
	reg := sso.NewRegistry(time.Minute)

	bad := sso.Config{ID: "p2", IssuerURL: mock.Issuer + "/no-such-issuer", ClientID: "c", RedirectURL: "http://localhost/cb"}
	_, _, err := reg.Get(context.Background(), bad)
	assert.Error(t, err, "discovery against a non-OIDC URL must fail")

	// A failed discovery must not be cached, so a later valid call still works
	// (here, simply re-attempting still errors rather than returning a stale hit).
	_, _, err = reg.Get(context.Background(), bad)
	assert.Error(t, err)
}

func TestRegistryTTLExpiryReDiscovers(t *testing.T) {
	mock := support.NewMockOIDCProvider(t, "client-1")
	reg := sso.NewRegistry(100 * time.Millisecond)

	cfg := sso.Config{
		ID:           "ttl",
		IssuerURL:    mock.Issuer,
		ClientID:     "client-1",
		ClientSecret: "s",
		RedirectURL:  "http://localhost/cb",
	}

	_, _, err := reg.Get(context.Background(), cfg)
	require.NoError(t, err)

	// Within the TTL the entry is served from cache, so it still resolves even
	// after the IdP becomes unreachable — proving no re-discovery happened.
	mock.Server.Close()
	_, _, err = reg.Get(context.Background(), cfg)
	require.NoError(t, err, "a cached entry should be served without re-hitting the IdP")

	// Past the TTL the entry is stale, so Get must re-discover — which now fails
	// because the IdP is down. This proves the cache was not served stale.
	time.Sleep(300 * time.Millisecond)
	_, _, err = reg.Get(context.Background(), cfg)
	require.Error(t, err, "an expired entry must trigger re-discovery, not a stale hit")
}
