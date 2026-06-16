package authentication

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

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

// ensureScope appends scope to scopes if it is not already present.
func ensureScope(scopes []string, scope string) []string {
	for _, s := range scopes {
		if s == scope {
			return scopes
		}
	}
	return append(scopes, scope)
}

func emailDomain(email string) string {
	at := strings.LastIndex(email, "@")
	if at < 0 {
		return ""
	}
	return strings.ToLower(email[at+1:])
}

// ssoAuthOptions reads optional OIDC authorization-request parameters from the
// login request, each gated by an installation-admin setting:
//   - login_hint: the caller-supplied hint (the email used for home-realm
//     discovery), forwarded so the IdP can pre-fill its login form.
//   - prompt=none: silent authentication. Only honored when the caller explicitly
//     asks for it (?prompt=none), so the interactive flow is unaffected; the
//     silent-login sequence is what sets it. Missing/disabled => no parameter.
func (a *Handler) ssoAuthOptions(r *http.Request) sso.AuthOptions {
	var opts sso.AuthOptions

	meta, err := models.GetInstallationMetadata()
	if err != nil {
		log.Warnf("SSO: failed to load installation metadata for auth options: %v", err)
		return opts
	}

	if meta.SSOLoginHintEnabled {
		opts.LoginHint = strings.TrimSpace(r.URL.Query().Get("login_hint"))
	}
	if meta.SSOPromptNoneEnabled && r.URL.Query().Get("prompt") == "none" {
		opts.PromptNone = true
	}

	return opts
}

// oidcConfig turns an OIDC provider into an sso.Config — decrypting the client
// secret and adding the `groups` scope when group features are configured.
func (a *Handler) oidcConfig(r *http.Request, provider *models.OrganizationOIDCProvider, orgID, slug string) (sso.Config, bool) {
	secret, err := provider.DecryptClientSecret(r.Context(), a.encryptor)
	if err != nil {
		log.Errorf("Failed to decrypt client secret for OIDC provider %s: %v", provider.ID, err)
		return sso.Config{}, false
	}

	scopes := []string(provider.Scopes)
	// Auto-request the conventional "groups" scope only when reading the default
	// "groups" claim; for a custom groups claim the admin supplies the scope.
	if provider.HasGroupFeatures() && provider.UsesDefaultGroupsClaim() {
		scopes = ensureScope(scopes, "groups")
	}

	return sso.Config{
		ID:           provider.ID.String(),
		IssuerURL:    provider.IssuerURL,
		ClientID:     provider.ClientID,
		ClientSecret: secret,
		RedirectURL:  ssoCallbackURL(orgID, slug),
		Scopes:       scopes,
		GroupsClaim:  provider.GroupsClaim,
	}, true
}

// authenticatorFor loads an enabled provider and returns an Authenticator for
// it, dispatching on the provider type. This is the seam where additional
// provider types (e.g. SAML) plug in — each type builds its own config and
// Authenticator implementation. Returns ok=false if the provider is missing,
// disabled, or of an unimplemented type.
func (a *Handler) authenticatorFor(r *http.Request, orgID, slug string) (*models.OrganizationOIDCProvider, sso.Authenticator, bool) {
	orgUUID, err := uuid.Parse(orgID)
	if err != nil {
		return nil, nil, false
	}

	provider, err := models.FindOIDCProviderBySlug(orgUUID, slug)
	if err != nil || !provider.Enabled {
		return nil, nil, false
	}

	switch provider.Type {
	case models.OIDCProviderTypeOIDC:
		cfg, ok := a.oidcConfig(r, provider, orgID, slug)
		if !ok {
			return nil, nil, false
		}
		return provider, a.ssoRegistry.Authenticator(cfg), true
	default:
		log.Warnf("SSO requested for unimplemented provider type %q (provider %s)", provider.Type, provider.ID)
		return nil, nil, false
	}
}

// handleSSOLogin starts the OIDC authorization-code flow for an organization's
// provider: GET /auth/sso/{orgId}/{providerSlug}
func (a *Handler) handleSSOLogin(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	orgID := vars["orgId"]
	slug := vars["providerSlug"]

	provider, authr, ok := a.authenticatorFor(r, orgID, slug)
	if !ok {
		ssoRedirectError(w, r, "provider_not_found")
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

	authURL, err := authr.AuthCodeURL(r.Context(), state, nonce, a.ssoAuthOptions(r))
	if err != nil {
		log.Errorf("OIDC discovery failed for org %s provider %s: %v", orgID, slug, err)
		ssoRedirectError(w, r, "provider_unavailable")
		return
	}

	if err := sso.SetStateCookie(w, r, a.jwtSigner, sso.State{
		State:        state,
		Nonce:        nonce,
		ProviderID:   provider.ID.String(),
		OrgID:        orgID,
		ProviderSlug: slug,
		Redirect:     getRedirectURL(r),
	}); err != nil {
		ssoRedirectError(w, r, "internal_error")
		return
	}

	http.Redirect(w, r, authURL, http.StatusTemporaryRedirect)
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

	// The IdP can return an authorization error instead of a code — notably
	// login_required / interaction_required when a silent (prompt=none) attempt
	// finds no usable session. Surface a stable reason so a silent-login caller
	// can fall back to an interactive sign-in. (Error responses echo state, so
	// the CSRF check above still applies.)
	if errParam := r.URL.Query().Get("error"); errParam != "" {
		log.Infof("SSO callback returned error for org %s provider %s: %s", orgID, slug, errParam)
		ssoRedirectError(w, r, ssoCallbackErrorReason(errParam))
		return
	}

	provider, authr, ok := a.authenticatorFor(r, orgID, slug)
	if !ok {
		ssoRedirectError(w, r, "provider_not_found")
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		ssoRedirectError(w, r, "missing_code")
		return
	}

	// Exchange the code and verify the identity (ID token + nonce). Verification
	// lives in the Authenticator; provisioning policy stays here in the handler.
	result, err := authr.Complete(r.Context(), code, st.Nonce)
	if err != nil {
		log.Errorf("SSO completion failed for org %s provider %s: %v", orgID, slug, err)
		ssoRedirectError(w, r, completeErrorReason(err))
		return
	}

	// Trust only verified emails: account matching, JIT provisioning, and the
	// domain gate all key on the email, so an unverified email is an account
	// takeover vector. Reject before any lookup/creation/linking. (Standard IdPs
	// such as Authelia, Keycloak, and Okta emit email_verified.)
	if !result.EmailVerified {
		ssoRedirectError(w, r, "email_not_verified")
		return
	}

	// Domain gate: when the provider restricts domains, the email must match.
	if !provider.AllowsEmailDomain(emailDomain(result.Email)) {
		ssoRedirectError(w, r, "domain_not_allowed")
		return
	}

	// Group gate: when the provider restricts groups, the user's IdP groups must
	// include at least one allowed group.
	if !provider.AllowsGroups(result.Groups) {
		ssoRedirectError(w, r, "group_not_allowed")
		return
	}

	gothUser := goth.User{
		Provider:     models.ProviderOIDCPrefix + provider.ID.String(),
		UserID:       result.Subject,
		Email:        result.Email,
		Name:         result.Name,
		NickName:     result.Username,
		AvatarURL:    result.AvatarURL,
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
		ExpiresAt:    result.ExpiresAt,
	}

	// A domain- or group-restricted match authorizes just-in-time provisioning;
	// otherwise fall back to the normal signup gate (invite / blockSignup).
	allowSignup := (len(provider.AllowedEmailDomains) > 0 && provider.AllowsEmailDomain(emailDomain(result.Email))) ||
		(len(provider.AllowedGroups) > 0 && provider.AllowsGroups(result.Groups))

	account, err := a.findOrCreateAccountForProvider(gothUser, allowSignup)
	if err != nil {
		if err.Error() == SignupDisabledError {
			ssoRedirectError(w, r, "signup_disabled")
			return
		}
		log.Errorf("Error finding/creating account for SSO login %s: %v", result.Email, err)
		ssoRedirectError(w, r, "internal_error")
		return
	}

	if err := updateAccountProviders(a.encryptor, account, gothUser); err != nil {
		log.Errorf("Error updating account provider for SSO login %s: %v", result.Email, err)
		ssoRedirectError(w, r, "internal_error")
		return
	}

	desiredRole := provider.ResolveRole(result.Groups)
	if desiredRole == "" {
		desiredRole = models.RoleOrgViewer
	}
	// With group->role mappings configured, the IdP is the source of truth, so
	// the role is re-synced on every login; otherwise it is set once at provision.
	syncRole := len(provider.GroupRoleMappings.Data()) > 0

	orgUUID, _ := uuid.Parse(orgID)
	if err := a.ensureOrgMembership(orgUUID, account, desiredRole, syncRole); err != nil {
		log.Errorf("Error provisioning org membership for SSO login %s: %v", result.Email, err)
		ssoRedirectError(w, r, "internal_error")
		return
	}

	if err := a.acceptPendingInvitations(account); err != nil {
		log.Errorf("Error accepting pending invitations for SSO login %s: %v", result.Email, err)
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

// completeErrorReason maps an Authenticator.Complete failure to a stable,
// user-facing sso_error reason (the detail is logged separately).
func completeErrorReason(err error) string {
	switch {
	case errors.Is(err, sso.ErrNonceMismatch):
		return "invalid_nonce"
	case errors.Is(err, sso.ErrMissingEmail):
		return "missing_email_claim"
	default:
		return "invalid_id_token"
	}
}

// ssoCallbackErrorReason maps an OIDC authorization-error code (RFC 6749 §4.1.2.1
// / OIDC Core §3.1.2.6) to a stable sso_error reason. The "no usable session"
// outcomes of a prompt=none attempt collapse to interaction_required so a
// silent-login caller knows to retry interactively.
func ssoCallbackErrorReason(errParam string) string {
	switch errParam {
	case "login_required", "interaction_required", "consent_required", "account_selection_required":
		return "interaction_required"
	default:
		return "sso_failed"
	}
}

// ensureOrgMembership performs just-in-time provisioning: if the account is not
// already an active member of the organization, create a human user and assign
// the default viewer role. Idempotent across repeat logins. Mirrors
// acceptInvitation's create-user-then-assign-role transaction.
func (a *Handler) ensureOrgMembership(orgID uuid.UUID, account *models.Account, desiredRole string, syncRole bool) error {
	if existing, err := models.FindActiveUserByEmail(orgID.String(), account.Email); err == nil {
		// Already a member. With group->role mappings configured the IdP is the
		// source of truth, so reconcile the role on every login.
		if syncRole {
			return a.reconcileOrgRole(existing.ID.String(), orgID.String(), desiredRole)
		}
		return nil
	}

	return database.Conn().Transaction(func(tx *gorm.DB) error {
		user, err := models.CreateUserInTransaction(tx, orgID, account.ID, account.Email, account.Name)
		if err != nil {
			return err
		}
		return a.authService.AssignRole(user.ID.String(), desiredRole, orgID.String(), models.DomainTypeOrganization)
	})
}

// reconcileOrgRole makes the user's organization role match desiredRole for
// IdP-driven RBAC. AssignRole replaces any existing role grant for the user in
// the domain, so this is a straight set — except we never auto-demote an
// organization owner (whose role is not group-managed).
func (a *Handler) reconcileOrgRole(userID, orgID, desiredRole string) error {
	roles, err := a.authService.GetUserRolesForOrg(context.Background(), userID, orgID)
	if err != nil {
		return err
	}

	for _, role := range roles {
		if role.Name == models.RoleOrgOwner {
			return nil
		}
	}

	return a.authService.AssignRole(userID, desiredRole, orgID, models.DomainTypeOrganization)
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
