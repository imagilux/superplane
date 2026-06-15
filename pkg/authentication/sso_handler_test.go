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
