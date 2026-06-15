package authentication

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/superplanehq/superplane/pkg/jwt"
	"github.com/superplanehq/superplane/pkg/models"
	"github.com/superplanehq/superplane/test/support"
)

func TestSSOProviderLookup(t *testing.T) {
	r := support.Setup(t)
	h := NewHandler(jwt.NewSigner("test-secret"), r.Encryptor, r.AuthService, "test", "/templates", false, false, false)

	mkProvider := func(slug, display string, enabled bool, domains []string) {
		p := models.NewOIDCProvider(r.Organization.ID, nil, slug, display, "", "https://"+slug, "cid", nil, domains, enabled)
		require.NoError(t, p.SetClientSecret(context.Background(), r.Encryptor, "secret"))
		require.NoError(t, p.Create())
	}
	mkProvider("idp", "Corp IdP", true, []string{"example.com"})
	mkProvider("idp-off", "Disabled IdP", false, []string{"example.com"})
	mkProvider("idp-open", "Unrestricted IdP", true, nil)

	lookup := func(email string) []any {
		target := "/auth/sso/providers"
		if email != "" {
			target += "?email=" + url.QueryEscape(email)
		}
		rec := httptest.NewRecorder()
		h.handleSSOProviderLookup(rec, httptest.NewRequest("GET", target, nil))
		require.Equal(t, http.StatusOK, rec.Code)
		var resp map[string]any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		return resp["providers"].([]any)
	}

	t.Run("returns only the enabled, domain-matched provider", func(t *testing.T) {
		providers := lookup("alice@example.com")
		require.Len(t, providers, 1)
		first := providers[0].(map[string]any)
		assert.Equal(t, "idp", first["providerSlug"])
		assert.Equal(t, "Corp IdP", first["displayName"])
		assert.Equal(t, r.Organization.Name, first["orgName"])
		assert.Equal(t, "/auth/sso/"+r.Organization.ID.String()+"/idp", first["loginUrl"])
	})

	t.Run("unknown domain returns empty", func(t *testing.T) {
		assert.Empty(t, lookup("user@nowhere.test"))
	})

	t.Run("empty email returns empty (no enumeration)", func(t *testing.T) {
		assert.Empty(t, lookup(""))
	})
}
