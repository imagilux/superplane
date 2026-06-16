package sso

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// AuthResult is the verified identity an Authenticator returns after completing
// a provider's login flow. It is transport- and protocol-agnostic: the OIDC
// implementation fills it from a verified ID token; a future SAML implementation
// would fill it from a verified assertion. Authorization/provisioning policy
// (domain/group gates, JIT, role resolution, sessions) is the caller's concern,
// not the Authenticator's.
type AuthResult struct {
	Subject       string // stable IdP subject identifier (sub)
	Email         string
	EmailVerified bool
	Name          string
	Username      string // preferred_username
	AvatarURL     string
	Groups        []string

	// Provider tokens, surfaced for account-provider linking.
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
}

// Sentinel errors a Complete implementation may return so callers can map them
// to stable, user-facing reasons.
var (
	ErrNonceMismatch = errors.New("sso: id token nonce mismatch")
	ErrMissingEmail  = errors.New("sso: id token has no email claim")
)

// AuthOptions carries optional OIDC authorization-request parameters. Both are
// gated by installation settings at the call site (see the SSO handler), so an
// implementation can emit them unconditionally when set.
type AuthOptions struct {
	// LoginHint, when non-empty, is sent as the OIDC `login_hint` parameter so the
	// IdP can pre-fill the username on its login form.
	LoginHint string
	// PromptNone, when true, sends `prompt=none` to request silent authentication:
	// the IdP completes without UI if a usable session exists, otherwise it returns
	// an error (e.g. login_required) instead of prompting.
	PromptNone bool
}

// Authenticator completes a provider's authorization flow. AuthCodeURL builds
// the redirect to the IdP; Complete exchanges the returned code and verifies the
// resulting identity (ID token + nonce, for OIDC). It is the seam the provider
// `type` dispatches to: add a new provider type (e.g. SAML) as another
// implementation here rather than branching inside the HTTP handler.
type Authenticator interface {
	AuthCodeURL(ctx context.Context, state, nonce string, opts AuthOptions) (string, error)
	Complete(ctx context.Context, code, nonce string) (*AuthResult, error)
}

// oidcAuthenticator implements Authenticator over the discovery Registry.
type oidcAuthenticator struct {
	registry *Registry
	config   Config
}

// Authenticator returns an OIDC Authenticator for the given provider config,
// backed by this registry's discovery cache and guarded HTTP client.
func (r *Registry) Authenticator(c Config) Authenticator {
	return &oidcAuthenticator{registry: r, config: c}
}

func (a *oidcAuthenticator) AuthCodeURL(ctx context.Context, state, nonce string, opts AuthOptions) (string, error) {
	oauthConfig, _, err := a.registry.Get(ctx, a.config)
	if err != nil {
		return "", err
	}

	params := []oauth2.AuthCodeOption{oidc.Nonce(nonce)}
	if opts.LoginHint != "" {
		params = append(params, oauth2.SetAuthURLParam("login_hint", opts.LoginHint))
	}
	if opts.PromptNone {
		params = append(params, oauth2.SetAuthURLParam("prompt", "none"))
	}

	return oauthConfig.AuthCodeURL(state, params...), nil
}

func (a *oidcAuthenticator) Complete(ctx context.Context, code, nonce string) (*AuthResult, error) {
	ctx = a.registry.ClientContext(ctx)

	oauthConfig, verifier, err := a.registry.Get(ctx, a.config)
	if err != nil {
		return nil, err
	}

	oauthToken, err := oauthConfig.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("sso: token exchange failed: %w", err)
	}

	rawIDToken, ok := oauthToken.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return nil, errors.New("sso: token response had no id_token")
	}

	idToken, err := verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, fmt.Errorf("sso: id token verification failed: %w", err)
	}

	// Replay protection: the nonce embedded in the ID token must match the one
	// the caller issued (and stored in the signed state cookie).
	if idToken.Nonce != nonce {
		return nil, ErrNonceMismatch
	}

	var raw map[string]any
	if err := idToken.Claims(&raw); err != nil {
		return nil, fmt.Errorf("sso: failed to parse id token claims: %w", err)
	}

	email := claimString(raw, "email")
	if email == "" {
		return nil, ErrMissingEmail
	}

	groupsClaim := a.config.GroupsClaim
	if groupsClaim == "" {
		groupsClaim = "groups"
	}

	return &AuthResult{
		Subject:       idToken.Subject,
		Email:         email,
		EmailVerified: claimBool(raw, "email_verified"),
		Name:          claimString(raw, "name"),
		Username:      claimString(raw, "preferred_username"),
		AvatarURL:     claimString(raw, "picture"),
		Groups:        claimStrings(raw, groupsClaim),
		AccessToken:   oauthToken.AccessToken,
		RefreshToken:  oauthToken.RefreshToken,
		ExpiresAt:     oauthToken.Expiry,
	}, nil
}

func claimString(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func claimBool(m map[string]any, key string) bool {
	if v, ok := m[key].(bool); ok {
		return v
	}
	return false
}

// claimStrings reads a string-list claim, tolerating both a JSON array of
// strings (the common case) and a single bare string (some IdPs emit one group
// as a string rather than a one-element array).
func claimStrings(m map[string]any, key string) []string {
	switch v := m[key].(type) {
	case []any:
		out := make([]string, 0, len(v))
		for _, e := range v {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case string:
		if v != "" {
			return []string{v}
		}
	}
	return nil
}
