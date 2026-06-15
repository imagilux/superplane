package authentication

import (
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/markbates/goth"
	log "github.com/sirupsen/logrus"
	"github.com/superplanehq/superplane/pkg/authentication/sso"
	"github.com/superplanehq/superplane/pkg/database"
	"github.com/superplanehq/superplane/pkg/models"
	"gorm.io/gorm"
)

// ssoCallbackURL builds the redirect URI for a provider from BASE_URL (required
// at boot). It must match the URI registered in the IdP.
func ssoCallbackURL(orgID, slug string) string {
	base := strings.TrimRight(os.Getenv("BASE_URL"), "/")
	return base + "/auth/sso/" + orgID + "/" + slug + "/callback"
}

func ssoRedirectError(w http.ResponseWriter, r *http.Request, reason string) {
	http.Redirect(w, r, "/login?sso_error="+url.QueryEscape(reason), http.StatusTemporaryRedirect)
}

func emailDomain(email string) string {
	at := strings.LastIndex(email, "@")
	if at < 0 {
		return ""
	}
	return strings.ToLower(email[at+1:])
}

// providerConfig loads an enabled OIDC provider and turns it into an sso.Config
// (decrypting the client secret). Returns ok=false if the provider is missing,
// disabled, or not an OIDC provider.
func (a *Handler) providerConfig(r *http.Request, orgID, slug string) (*models.OrganizationOIDCProvider, sso.Config, bool) {
	orgUUID, err := uuid.Parse(orgID)
	if err != nil {
		return nil, sso.Config{}, false
	}

	provider, err := models.FindOIDCProviderBySlug(orgUUID, slug)
	if err != nil || !provider.Enabled || provider.Type != models.OIDCProviderTypeOIDC {
		return nil, sso.Config{}, false
	}

	secret, err := provider.DecryptClientSecret(r.Context(), a.encryptor)
	if err != nil {
		log.Errorf("Failed to decrypt client secret for OIDC provider %s: %v", provider.ID, err)
		return nil, sso.Config{}, false
	}

	cfg := sso.Config{
		ID:           provider.ID.String(),
		IssuerURL:    provider.IssuerURL,
		ClientID:     provider.ClientID,
		ClientSecret: secret,
		RedirectURL:  ssoCallbackURL(orgID, slug),
		Scopes:       []string(provider.Scopes),
	}
	return provider, cfg, true
}

// handleSSOLogin starts the OIDC authorization-code flow for an organization's
// provider: GET /auth/sso/{orgId}/{providerSlug}
func (a *Handler) handleSSOLogin(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	orgID := vars["orgId"]
	slug := vars["providerSlug"]

	_, cfg, ok := a.providerConfig(r, orgID, slug)
	if !ok {
		ssoRedirectError(w, r, "provider_not_found")
		return
	}

	oauthConfig, _, err := a.ssoRegistry.Get(r.Context(), cfg)
	if err != nil {
		log.Errorf("OIDC discovery failed for org %s provider %s: %v", orgID, slug, err)
		ssoRedirectError(w, r, "provider_unavailable")
		return
	}

	state, err := sso.RandomToken()
	if err != nil {
		ssoRedirectError(w, r, "internal_error")
		return
	}
	nonce, err := sso.RandomToken()
	if err != nil {
		ssoRedirectError(w, r, "internal_error")
		return
	}

	if err := sso.SetStateCookie(w, r, a.jwtSigner, sso.State{
		State:        state,
		Nonce:        nonce,
		ProviderID:   cfg.ID,
		OrgID:        orgID,
		ProviderSlug: slug,
		Redirect:     getRedirectURL(r),
	}); err != nil {
		ssoRedirectError(w, r, "internal_error")
		return
	}

	http.Redirect(w, r, oauthConfig.AuthCodeURL(state, oidc.Nonce(nonce)), http.StatusTemporaryRedirect)
}

// handleSSOCallback completes the flow: GET /auth/sso/{orgId}/{providerSlug}/callback
func (a *Handler) handleSSOCallback(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	orgID := vars["orgId"]
	slug := vars["providerSlug"]

	st, err := sso.ReadStateCookie(r, a.jwtSigner)
	sso.ClearStateCookie(w)
	if err != nil {
		ssoRedirectError(w, r, "invalid_state")
		return
	}

	// CSRF: the state in the cookie must match the state echoed by the IdP, and
	// the callback path must match the org/provider the flow was started for.
	if st.State == "" || st.State != r.URL.Query().Get("state") || st.OrgID != orgID || st.ProviderSlug != slug {
		ssoRedirectError(w, r, "invalid_state")
		return
	}

	provider, cfg, ok := a.providerConfig(r, orgID, slug)
	if !ok {
		ssoRedirectError(w, r, "provider_not_found")
		return
	}

	ctx := a.ssoRegistry.ClientContext(r.Context())
	oauthConfig, verifier, err := a.ssoRegistry.Get(ctx, cfg)
	if err != nil {
		log.Errorf("OIDC discovery failed on callback for org %s provider %s: %v", orgID, slug, err)
		ssoRedirectError(w, r, "provider_unavailable")
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		ssoRedirectError(w, r, "missing_code")
		return
	}

	oauthToken, err := oauthConfig.Exchange(ctx, code)
	if err != nil {
		log.Errorf("OIDC token exchange failed for org %s provider %s: %v", orgID, slug, err)
		ssoRedirectError(w, r, "exchange_failed")
		return
	}

	rawIDToken, ok := oauthToken.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		ssoRedirectError(w, r, "no_id_token")
		return
	}

	idToken, err := verifier.Verify(ctx, rawIDToken)
	if err != nil {
		log.Errorf("OIDC ID token verification failed for org %s provider %s: %v", orgID, slug, err)
		ssoRedirectError(w, r, "invalid_id_token")
		return
	}

	// Replay protection: the nonce embedded in the ID token must match the one we
	// generated and stored in the signed state cookie.
	if idToken.Nonce != st.Nonce {
		ssoRedirectError(w, r, "invalid_nonce")
		return
	}

	var claims struct {
		Email             string `json:"email"`
		EmailVerified     bool   `json:"email_verified"`
		Name              string `json:"name"`
		PreferredUsername string `json:"preferred_username"`
		Picture           string `json:"picture"`
	}
	if err := idToken.Claims(&claims); err != nil || claims.Email == "" {
		ssoRedirectError(w, r, "missing_email_claim")
		return
	}

	// Trust only verified emails: account matching, JIT provisioning, and the
	// domain gate all key on the email, so an unverified email is an account
	// takeover vector. Reject before any lookup/creation/linking. (Standard IdPs
	// such as Authelia, Keycloak, and Okta emit email_verified.)
	if !claims.EmailVerified {
		ssoRedirectError(w, r, "email_not_verified")
		return
	}

	// Domain gate: when the provider restricts domains, the email must match.
	if !provider.AllowsEmailDomain(emailDomain(claims.Email)) {
		ssoRedirectError(w, r, "domain_not_allowed")
		return
	}

	gothUser := goth.User{
		Provider:     models.ProviderOIDCPrefix + provider.ID.String(),
		UserID:       idToken.Subject,
		Email:        claims.Email,
		Name:         claims.Name,
		NickName:     claims.PreferredUsername,
		AvatarURL:    claims.Picture,
		AccessToken:  oauthToken.AccessToken,
		RefreshToken: oauthToken.RefreshToken,
		ExpiresAt:    oauthToken.Expiry,
	}

	// A successful domain-restricted login authorizes just-in-time provisioning;
	// otherwise fall back to the normal signup gate (invite / blockSignup).
	allowSignup := len(provider.AllowedEmailDomains) > 0 && provider.AllowsEmailDomain(emailDomain(claims.Email))

	account, err := a.findOrCreateAccountForProvider(gothUser, allowSignup)
	if err != nil {
		if err.Error() == SignupDisabledError {
			ssoRedirectError(w, r, "signup_disabled")
			return
		}
		log.Errorf("Error finding/creating account for SSO login %s: %v", claims.Email, err)
		ssoRedirectError(w, r, "internal_error")
		return
	}

	if err := updateAccountProviders(a.encryptor, account, gothUser); err != nil {
		log.Errorf("Error updating account provider for SSO login %s: %v", claims.Email, err)
		ssoRedirectError(w, r, "internal_error")
		return
	}

	orgUUID, _ := uuid.Parse(orgID)
	if err := a.ensureOrgMembership(orgUUID, account); err != nil {
		log.Errorf("Error provisioning org membership for SSO login %s: %v", claims.Email, err)
		ssoRedirectError(w, r, "internal_error")
		return
	}

	if err := a.acceptPendingInvitations(account); err != nil {
		log.Errorf("Error accepting pending invitations for SSO login %s: %v", claims.Email, err)
		ssoRedirectError(w, r, "internal_error")
		return
	}

	if err := IssueAccountSession(w, r, a.jwtSigner, account.ID.String()); err != nil {
		ssoRedirectError(w, r, "internal_error")
		return
	}

	redirect := st.Redirect
	if redirect == "" || !isValidRedirectURL(redirect) {
		redirect = "/"
	}
	http.Redirect(w, r, redirect, http.StatusTemporaryRedirect)
}

// ensureOrgMembership performs just-in-time provisioning: if the account is not
// already an active member of the organization, create a human user and assign
// the default viewer role. Idempotent across repeat logins. Mirrors
// acceptInvitation's create-user-then-assign-role transaction.
func (a *Handler) ensureOrgMembership(orgID uuid.UUID, account *models.Account) error {
	if _, err := models.FindActiveUserByEmail(orgID.String(), account.Email); err == nil {
		return nil
	}

	return database.Conn().Transaction(func(tx *gorm.DB) error {
		user, err := models.CreateUserInTransaction(tx, orgID, account.ID, account.Email, account.Name)
		if err != nil {
			return err
		}
		return a.authService.AssignRole(user.ID.String(), models.RoleOrgViewer, orgID.String(), models.DomainTypeOrganization)
	})
}

// ssoDiscoveryTTL is how long OIDC discovery results are cached. Override with
// SSO_DISCOVERY_TTL (a Go duration); defaults to 10 minutes.
func ssoDiscoveryTTL() time.Duration {
	if v := os.Getenv("SSO_DISCOVERY_TTL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return 10 * time.Minute
}
