package oidcproviders

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	pb "github.com/superplanehq/superplane/pkg/protos/oidc_providers"
	"github.com/superplanehq/superplane/test/support"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func authCtx(orgID, userID string) context.Context {
	return metadata.NewIncomingContext(
		context.Background(),
		metadata.Pairs("x-organization-id", orgID, "x-user-id", userID),
	)
}

func TestCreateOIDCProvider(t *testing.T) {
	r := support.Setup(t)
	ctx := authCtx(r.Organization.ID.String(), r.User.String())

	t.Run("creates a provider and never returns the secret", func(t *testing.T) {
		resp, err := CreateOIDCProvider(ctx, &pb.CreateOIDCProviderRequest{
			Slug:                "authelia",
			DisplayName:         "Authelia",
			IssuerUrl:           "https://auth.example.com",
			ClientId:            "client-abc",
			ClientSecret:        "super-secret",
			AllowedEmailDomains: []string{"example.com"},
			Enabled:             true,
		}, r.Encryptor)
		require.NoError(t, err)
		assert.Equal(t, "authelia", resp.Provider.Slug)
		assert.Equal(t, "oidc", resp.Provider.Type)
		assert.True(t, resp.Provider.HasClientSecret)
		assert.True(t, resp.Provider.Enabled)
		assert.ElementsMatch(t, []string{"openid", "email", "profile"}, resp.Provider.Scopes)
		assert.Equal(t, r.Organization.ID.String(), resp.Provider.OrganizationId)
	})

	t.Run("rejects SAML as not yet implemented", func(t *testing.T) {
		_, err := CreateOIDCProvider(ctx, &pb.CreateOIDCProviderRequest{
			Slug: "okta", DisplayName: "Okta", Type: "saml",
			IssuerUrl: "https://okta", ClientId: "c", ClientSecret: "s",
		}, r.Encryptor)
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("validates required fields and slug format", func(t *testing.T) {
		cases := []*pb.CreateOIDCProviderRequest{
			{Slug: "", DisplayName: "x", IssuerUrl: "https://x", ClientId: "c", ClientSecret: "s"},
			{Slug: "Bad Slug", DisplayName: "x", IssuerUrl: "https://x", ClientId: "c", ClientSecret: "s"},
			{Slug: "ok", DisplayName: "", IssuerUrl: "https://x", ClientId: "c", ClientSecret: "s"},
			{Slug: "ok", DisplayName: "x", IssuerUrl: "", ClientId: "c", ClientSecret: "s"},
			{Slug: "ok", DisplayName: "x", IssuerUrl: "https://x", ClientId: "", ClientSecret: "s"},
			{Slug: "ok", DisplayName: "x", IssuerUrl: "https://x", ClientId: "c", ClientSecret: ""},
		}
		for _, req := range cases {
			_, err := CreateOIDCProvider(ctx, req, r.Encryptor)
			assert.Equal(t, codes.InvalidArgument, status.Code(err), "req %+v", req)
		}
	})

	t.Run("rejects duplicate slug in the same org", func(t *testing.T) {
		req := &pb.CreateOIDCProviderRequest{Slug: "dup", DisplayName: "D", IssuerUrl: "https://x", ClientId: "c", ClientSecret: "s"}
		_, err := CreateOIDCProvider(ctx, req, r.Encryptor)
		require.NoError(t, err)
		_, err = CreateOIDCProvider(ctx, req, r.Encryptor)
		assert.Equal(t, codes.AlreadyExists, status.Code(err))
	})

	t.Run("requires authentication", func(t *testing.T) {
		_, err := CreateOIDCProvider(context.Background(), &pb.CreateOIDCProviderRequest{Slug: "x"}, r.Encryptor)
		assert.Equal(t, codes.Unauthenticated, status.Code(err))
	})
}

func TestOIDCProviderLifecycle(t *testing.T) {
	r := support.Setup(t)
	ctx := authCtx(r.Organization.ID.String(), r.User.String())

	created, err := CreateOIDCProvider(ctx, &pb.CreateOIDCProviderRequest{
		Slug: "kc", DisplayName: "Keycloak", IssuerUrl: "https://kc.example.com",
		ClientId: "cid", ClientSecret: "sec", Enabled: true,
	}, r.Encryptor)
	require.NoError(t, err)
	id := created.Provider.Id

	t.Run("list returns the org's providers", func(t *testing.T) {
		resp, err := ListOIDCProviders(ctx)
		require.NoError(t, err)
		require.Len(t, resp.Providers, 1)
		assert.Equal(t, "kc", resp.Providers[0].Slug)
	})

	t.Run("describe returns the provider; unknown id is NotFound", func(t *testing.T) {
		resp, err := DescribeOIDCProvider(ctx, &pb.DescribeOIDCProviderRequest{Id: id})
		require.NoError(t, err)
		assert.Equal(t, "Keycloak", resp.Provider.DisplayName)

		_, err = DescribeOIDCProvider(ctx, &pb.DescribeOIDCProviderRequest{Id: uuid.NewString()})
		assert.Equal(t, codes.NotFound, status.Code(err))
	})

	t.Run("update changes fields and keeps the secret when omitted", func(t *testing.T) {
		resp, err := UpdateOIDCProvider(ctx, &pb.UpdateOIDCProviderRequest{
			Id: id, DisplayName: "Keycloak v2", IssuerUrl: "https://kc2.example.com",
			ClientId: "cid2", Enabled: false, ClientSecret: "",
		}, r.Encryptor)
		require.NoError(t, err)
		assert.Equal(t, "Keycloak v2", resp.Provider.DisplayName)
		assert.Equal(t, "https://kc2.example.com", resp.Provider.IssuerUrl)
		assert.False(t, resp.Provider.Enabled)
		assert.True(t, resp.Provider.HasClientSecret, "secret should be preserved when omitted")
	})

	t.Run("delete removes the provider", func(t *testing.T) {
		_, err := DeleteOIDCProvider(ctx, &pb.DeleteOIDCProviderRequest{Id: id})
		require.NoError(t, err)

		_, err = DescribeOIDCProvider(ctx, &pb.DescribeOIDCProviderRequest{Id: id})
		assert.Equal(t, codes.NotFound, status.Code(err))
	})
}
