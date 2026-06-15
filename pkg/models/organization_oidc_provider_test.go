package models

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/superplanehq/superplane/pkg/crypto"
	"github.com/superplanehq/superplane/pkg/database"
)

func createTestOrg(t *testing.T, name string) *Organization {
	org := &Organization{ID: uuid.New(), Name: name}
	require.NoError(t, database.Conn().Create(org).Error)
	return org
}

func TestOrganizationOIDCProvider_CreateAndFind(t *testing.T) {
	require.NoError(t, database.TruncateTables())
	ctx := context.Background()
	enc := crypto.NewAESGCMEncryptor([]byte("1234567890abcdefghijklmnopqrstuv"))

	org := createTestOrg(t, "Acme")

	t.Run("create encrypts the secret and round-trips", func(t *testing.T) {
		p := NewOIDCProvider(org.ID, nil, "authelia", "Authelia", "", "https://auth.example.com", "client-abc",
			nil, []string{"example.com"}, true)
		require.NoError(t, p.SetClientSecret(ctx, enc, "s3cr3t-value"))
		require.NoError(t, p.Create())

		// Secret is not stored in plaintext.
		assert.NotEmpty(t, p.ClientSecretEnc)
		assert.NotContains(t, p.ClientSecretEnc, "s3cr3t-value")
		assert.True(t, p.HasClientSecret())

		// Defaults applied.
		assert.Equal(t, OIDCProviderTypeOIDC, p.Type)
		assert.Equal(t, DefaultOIDCScopes, []string(p.Scopes))

		found, err := FindOIDCProviderByID(org.ID, p.ID)
		require.NoError(t, err)
		assert.Equal(t, "authelia", found.Slug)
		assert.Equal(t, "https://auth.example.com", found.IssuerURL)

		secret, err := found.DecryptClientSecret(ctx, enc)
		require.NoError(t, err)
		assert.Equal(t, "s3cr3t-value", secret)
	})

	t.Run("find by slug, scoped to organization", func(t *testing.T) {
		other := createTestOrg(t, "Globex")

		found, err := FindOIDCProviderBySlug(org.ID, "authelia")
		require.NoError(t, err)
		assert.Equal(t, org.ID, found.OrganizationID)

		_, err = FindOIDCProviderBySlug(other.ID, "authelia")
		assert.Error(t, err, "provider must not be visible from another org")
	})

	t.Run("slug is unique per organization", func(t *testing.T) {
		dup := NewOIDCProvider(org.ID, nil, "authelia", "Dup", "", "https://dup.example.com", "client-dup",
			nil, nil, true)
		require.NoError(t, dup.SetClientSecret(ctx, enc, "x"))
		err := dup.Create()
		assert.ErrorIs(t, err, ErrNameAlreadyUsed)
	})
}

func TestOrganizationOIDCProvider_ListAndSoftDelete(t *testing.T) {
	require.NoError(t, database.TruncateTables())
	ctx := context.Background()
	enc := crypto.NewNoOpEncryptor()
	org := createTestOrg(t, "Acme")

	p := NewOIDCProvider(org.ID, nil, "kc", "Keycloak", "", "https://kc.example.com", "cid", nil, nil, true)
	require.NoError(t, p.SetClientSecret(ctx, enc, "secret"))
	require.NoError(t, p.Create())

	list, err := FindOIDCProvidersByOrganization(org.ID)
	require.NoError(t, err)
	assert.Len(t, list, 1)

	t.Run("soft delete hides the row and frees the slug", func(t *testing.T) {
		require.NoError(t, p.Delete())

		_, err := FindOIDCProviderByID(org.ID, p.ID)
		assert.Error(t, err)

		list, err := FindOIDCProvidersByOrganization(org.ID)
		require.NoError(t, err)
		assert.Empty(t, list)

		// Slug can be reused after soft delete.
		reuse := NewOIDCProvider(org.ID, nil, "kc", "Keycloak v2", "", "https://kc2.example.com", "cid2", nil, nil, true)
		require.NoError(t, reuse.SetClientSecret(ctx, enc, "secret2"))
		require.NoError(t, reuse.Create())
	})
}

func TestOrganizationOIDCProvider_EmailDomainDiscovery(t *testing.T) {
	require.NoError(t, database.TruncateTables())
	ctx := context.Background()
	enc := crypto.NewNoOpEncryptor()
	org := createTestOrg(t, "Acme")

	mk := func(slug string, enabled bool, domains []string) *OrganizationOIDCProvider {
		p := NewOIDCProvider(org.ID, nil, slug, slug, "", "https://"+slug+".example.com", "cid", nil, domains, enabled)
		require.NoError(t, p.SetClientSecret(ctx, enc, "secret"))
		require.NoError(t, p.Create())
		return p
	}

	mk("matched", true, []string{"example.com"})
	mk("disabled", false, []string{"example.com"})
	mk("unrestricted", true, nil)
	mk("otherdomain", true, []string{"other.org"})

	t.Run("returns only enabled, domain-matched providers", func(t *testing.T) {
		// Case-insensitive on the queried domain.
		got, err := FindEnabledOIDCProvidersByEmailDomain("Example.COM")
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, "matched", got[0].Slug)
	})

	t.Run("no match for unknown or empty domain", func(t *testing.T) {
		got, err := FindEnabledOIDCProvidersByEmailDomain("nope.com")
		require.NoError(t, err)
		assert.Empty(t, got)

		got, err = FindEnabledOIDCProvidersByEmailDomain("")
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("AllowsEmailDomain semantics", func(t *testing.T) {
		restricted := NewOIDCProvider(org.ID, nil, "r", "r", "", "https://r", "cid", nil, []string{"example.com"}, true)
		assert.True(t, restricted.AllowsEmailDomain("Example.com"))
		assert.False(t, restricted.AllowsEmailDomain("evil.com"))

		open := NewOIDCProvider(org.ID, nil, "o", "o", "", "https://o", "cid", nil, nil, true)
		assert.True(t, open.AllowsEmailDomain("anything.com"))
	})
}

func TestOrganizationOIDCProvider_Groups(t *testing.T) {
	org := uuid.New()

	t.Run("AllowsGroups: empty allows all, else requires membership", func(t *testing.T) {
		p := NewOIDCProvider(org, nil, "g", "g", "", "https://i", "c", nil, nil, true)
		assert.True(t, p.AllowsGroups([]string{"anything"}))
		assert.True(t, p.AllowsGroups(nil))

		p.SetAllowedGroups([]string{"devs", "ops"})
		assert.True(t, p.AllowsGroups([]string{"ops"}))
		assert.True(t, p.AllowsGroups([]string{"x", "devs"}))
		assert.False(t, p.AllowsGroups([]string{"x"}))
		assert.False(t, p.AllowsGroups(nil))
	})

	t.Run("ResolveRole: highest precedence wins; no match is empty", func(t *testing.T) {
		p := NewOIDCProvider(org, nil, "g", "g", "", "https://i", "c", nil, nil, true)
		assert.Equal(t, "", p.ResolveRole([]string{"devs"}))
		assert.False(t, p.HasGroupFeatures())

		p.SetGroupRoleMappings(map[string]string{"admins": RoleOrgAdmin, "viewers": RoleOrgViewer})
		assert.True(t, p.HasGroupFeatures())
		assert.Equal(t, RoleOrgViewer, p.ResolveRole([]string{"viewers"}))
		assert.Equal(t, RoleOrgAdmin, p.ResolveRole([]string{"admins"}))
		assert.Equal(t, RoleOrgAdmin, p.ResolveRole([]string{"viewers", "admins"}))
		assert.Equal(t, "", p.ResolveRole([]string{"other"}))
	})
}
