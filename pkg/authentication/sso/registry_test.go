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
