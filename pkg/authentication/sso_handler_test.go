package authentication

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/superplanehq/superplane/pkg/authentication/sso"
	"github.com/superplanehq/superplane/pkg/authorization"
	"github.com/superplanehq/superplane/pkg/jwt"
	"github.com/superplanehq/superplane/pkg/models"
	"github.com/superplanehq/superplane/test/support"
)

func setupSSO(t *testing.T) (*Handler, *support.ResourceRegistry, *support.MockOIDCProvider, string, string, *models.OrganizationOIDCProvider) {
	r := support.Setup(t)
	t.Setenv("BASE_URL", "http://localhost:8000")
	mock := support.NewMockOIDCProvider(t, "test-client")

	provider := models.NewOIDCProvider(r.Organization.ID, nil, "idp", "Test IdP", "", mock.Issuer, "test-client", nil, []string{"example.com"}, true)
	require.NoError(t, provider.SetClientSecret(context.Background(), r.Encryptor, "test-secret"))
	require.NoError(t, provider.Create())

	h := NewHandler(jwt.NewSigner("test-secret"), r.Encryptor, r.AuthService, "test", "/templates", false, false, false)
	return h, r, mock, r.Organization.ID.String(), "idp", provider
}

func doSSOLogin(t *testing.T, h *Handler, orgID, slug, redirect string) (string, string, *http.Cookie) {
	target := "/auth/sso/" + orgID + "/" + slug
	if redirect != "" {
		target += "?redirect=" + url.QueryEscape(redirect)
	}
	req := mux.SetURLVars(httptest.NewRequest("GET", target, nil), map[string]string{"orgId": orgID, "providerSlug": slug})
	rec := httptest.NewRecorder()
	h.handleSSOLogin(rec, req)

	require.Equal(t, http.StatusTemporaryRedirect, rec.Code, "login should redirect to the IdP authorize endpoint")
	loc, err := url.Parse(rec.Header().Get("Location"))
	require.NoError(t, err)
	state := loc.Query().Get("state")
	nonce := loc.Query().Get("nonce")
	require.NotEmpty(t, state)
	require.NotEmpty(t, nonce)

	var cookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == sso.StateCookieName {
			cookie = c
		}
	}
	require.NotNil(t, cookie, "state cookie must be set on login")
	return state, nonce, cookie
}

func doSSOCallback(h *Handler, orgID, slug, code, state string, cookie *http.Cookie) *httptest.ResponseRecorder {
	target := "/auth/sso/" + orgID + "/" + slug + "/callback?code=" + url.QueryEscape(code) + "&state=" + url.QueryEscape(state)
	req := mux.SetURLVars(httptest.NewRequest("GET", target, nil), map[string]string{"orgId": orgID, "providerSlug": slug})
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	h.handleSSOCallback(rec, req)
	return rec
}

func hasCookie(rec *httptest.ResponseRecorder, name string) bool {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name && c.Value != "" {
			return true
		}
	}
	return false
}

func hasRole(roles []*authorization.RoleDefinition, name string) bool {
	for _, role := range roles {
		if role.Name == name {
			return true
		}
	}
	return false
}

func TestSSOFlow_HappyPath(t *testing.T) {
	h, r, mock, orgID, slug, provider := setupSSO(t)

	state, nonce, cookie := doSSOLogin(t, h, orgID, slug, "/dashboard")

	code := "code-ok"
	mock.RegisterCode(code, support.MockIDClaims{Sub: "sub-alice", Email: "alice@example.com", Name: "Alice", Nonce: nonce, EmailVerified: true})

	rec := doSSOCallback(h, orgID, slug, code, state, cookie)

	require.Equal(t, http.StatusTemporaryRedirect, rec.Code)
	assert.Equal(t, "/dashboard", rec.Header().Get("Location"))
	assert.True(t, hasCookie(rec, "account_token"), "a session cookie should be issued")

	account, err := models.FindAccountByEmail("alice@example.com")
	require.NoError(t, err)

	ap, err := account.FindAccountProviderByID(models.ProviderOIDCPrefix+provider.ID.String(), "sub-alice")
	require.NoError(t, err)
	assert.Equal(t, "alice@example.com", ap.Email)

	// JIT provisioning: a human user and the default viewer role.
	user, err := models.FindActiveUserByEmail(orgID, account.Email)
	require.NoError(t, err)
	roles, err := r.AuthService.GetUserRolesForOrg(context.Background(), user.ID.String(), orgID)
	require.NoError(t, err)
	assert.True(t, hasRole(roles, models.RoleOrgViewer), "SSO user should get the org_viewer role")
}

func TestSSOFlow_DomainGateRejectsDisallowedEmail(t *testing.T) {
	h, _, mock, orgID, slug, _ := setupSSO(t)

	state, nonce, cookie := doSSOLogin(t, h, orgID, slug, "")
	mock.RegisterCode("code-bad", support.MockIDClaims{Sub: "sub-bob", Email: "bob@evil.com", Nonce: nonce, EmailVerified: true})

	rec := doSSOCallback(h, orgID, slug, "code-bad", state, cookie)

	require.Equal(t, http.StatusTemporaryRedirect, rec.Code)
	assert.Contains(t, rec.Header().Get("Location"), "sso_error=domain_not_allowed")
	_, err := models.FindAccountByEmail("bob@evil.com")
	assert.Error(t, err, "no account should be provisioned for a disallowed domain")
}

func TestSSOFlow_UnverifiedEmailRejected(t *testing.T) {
	h, _, mock, orgID, slug, _ := setupSSO(t)

	state, nonce, cookie := doSSOLogin(t, h, orgID, slug, "")
	mock.RegisterCode("code-unverified", support.MockIDClaims{Sub: "sub-carol", Email: "carol@example.com", Nonce: nonce, EmailVerified: false})

	rec := doSSOCallback(h, orgID, slug, "code-unverified", state, cookie)

	require.Equal(t, http.StatusTemporaryRedirect, rec.Code)
	assert.Contains(t, rec.Header().Get("Location"), "sso_error=email_not_verified")
	_, err := models.FindAccountByEmail("carol@example.com")
	assert.Error(t, err, "an unverified email must not be provisioned")
}

func TestSSOFlow_NonceMismatchRejected(t *testing.T) {
	h, _, mock, orgID, slug, _ := setupSSO(t)

	state, _, cookie := doSSOLogin(t, h, orgID, slug, "")
	// IdP returns a token whose nonce does not match the one we issued.
	mock.RegisterCode("code-replay", support.MockIDClaims{Sub: "sub-x", Email: "x@example.com", Nonce: "WRONG-NONCE"})

	rec := doSSOCallback(h, orgID, slug, "code-replay", state, cookie)

	require.Equal(t, http.StatusTemporaryRedirect, rec.Code)
	assert.Contains(t, rec.Header().Get("Location"), "sso_error=invalid_nonce")
}

func TestSSOFlow_CSRFStateMismatchRejected(t *testing.T) {
	h, _, _, orgID, slug, _ := setupSSO(t)

	_, _, cookie := doSSOLogin(t, h, orgID, slug, "")
	// A state that does not match the signed cookie is rejected before any token exchange.
	rec := doSSOCallback(h, orgID, slug, "code-x", "ATTACKER-STATE", cookie)

	require.Equal(t, http.StatusTemporaryRedirect, rec.Code)
	assert.Contains(t, rec.Header().Get("Location"), "sso_error=invalid_state")
}

func TestSSOLogin_UnknownProviderRedirects(t *testing.T) {
	h, _, _, orgID, _, _ := setupSSO(t)

	req := mux.SetURLVars(
		httptest.NewRequest("GET", "/auth/sso/"+orgID+"/nope", nil),
		map[string]string{"orgId": orgID, "providerSlug": "nope"},
	)
	rec := httptest.NewRecorder()
	h.handleSSOLogin(rec, req)

	require.Equal(t, http.StatusTemporaryRedirect, rec.Code)
	assert.Contains(t, rec.Header().Get("Location"), "sso_error=provider_not_found")
}

func TestSSOFlow_GroupGate(t *testing.T) {
	h, r, mock, orgID, _, _ := setupSSO(t)

	p := models.NewOIDCProvider(r.Organization.ID, nil, "gated", "Gated", "", mock.Issuer, "test-client", nil, nil, true)
	p.SetAllowedGroups([]string{"devs"})
	require.NoError(t, p.SetClientSecret(context.Background(), r.Encryptor, "test-secret"))
	require.NoError(t, p.Create())

	t.Run("rejects a user not in an allowed group", func(t *testing.T) {
		state, nonce, cookie := doSSOLogin(t, h, orgID, "gated", "")
		mock.RegisterCode("g-no", support.MockIDClaims{Sub: "s1", Email: "a@example.com", Nonce: nonce, EmailVerified: true, Groups: []string{"other"}})
		rec := doSSOCallback(h, orgID, "gated", "g-no", state, cookie)
		assert.Contains(t, rec.Header().Get("Location"), "sso_error=group_not_allowed")
	})

	t.Run("admits a user in an allowed group", func(t *testing.T) {
		state, nonce, cookie := doSSOLogin(t, h, orgID, "gated", "")
		mock.RegisterCode("g-yes", support.MockIDClaims{Sub: "s2", Email: "b@example.com", Nonce: nonce, EmailVerified: true, Groups: []string{"devs"}})
		rec := doSSOCallback(h, orgID, "gated", "g-yes", state, cookie)
		require.Equal(t, http.StatusTemporaryRedirect, rec.Code)
		assert.NotContains(t, rec.Header().Get("Location"), "sso_error")
	})
}

func TestSSOFlow_RoleMappingAndResync(t *testing.T) {
	h, r, mock, orgID, _, _ := setupSSO(t)
	ctx := context.Background()

	p := models.NewOIDCProvider(r.Organization.ID, nil, "rbac", "RBAC", "", mock.Issuer, "test-client", nil, nil, true)
	p.SetGroupRoleMappings(map[string]string{"admins": models.RoleOrgAdmin, "viewers": models.RoleOrgViewer})
	require.NoError(t, p.SetClientSecret(ctx, r.Encryptor, "test-secret"))
	require.NoError(t, p.Create())

	login := func(code string, groups []string) {
		state, nonce, cookie := doSSOLogin(t, h, orgID, "rbac", "")
		mock.RegisterCode(code, support.MockIDClaims{Sub: "sub-rb", Email: "rb@example.com", Nonce: nonce, EmailVerified: true, Groups: groups})
		rec := doSSOCallback(h, orgID, "rbac", code, state, cookie)
		require.Equal(t, http.StatusTemporaryRedirect, rec.Code)
		require.NotContains(t, rec.Header().Get("Location"), "sso_error")
	}
	rolesOf := func() []*authorization.RoleDefinition {
		user, err := models.FindActiveUserByEmail(orgID, "rb@example.com")
		require.NoError(t, err)
		roles, err := r.AuthService.GetUserRolesForOrg(ctx, user.ID.String(), orgID)
		require.NoError(t, err)
		return roles
	}

	t.Run("first login maps the group to viewer", func(t *testing.T) {
		login("rb1", []string{"viewers"})
		assert.True(t, hasRole(rolesOf(), models.RoleOrgViewer))
		assert.False(t, hasRole(rolesOf(), models.RoleOrgAdmin), "viewer must not imply admin")
	})

	t.Run("re-login with the admin group upgrades the role to admin", func(t *testing.T) {
		login("rb2", []string{"admins"})
		assert.True(t, hasRole(rolesOf(), models.RoleOrgAdmin))
	})

	t.Run("re-login with the viewer group downgrades, dropping the admin grant", func(t *testing.T) {
		// viewer does not imply admin, so admin's absence proves the prior
		// admin grant was actually removed (not merely shadowed by inheritance).
		login("rb3", []string{"viewers"})
		roles := rolesOf()
		assert.True(t, hasRole(roles, models.RoleOrgViewer))
		assert.False(t, hasRole(roles, models.RoleOrgAdmin), "admin grant must be removed on downgrade")
	})
}
