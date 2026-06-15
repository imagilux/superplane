package sso

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"time"

	"github.com/superplanehq/superplane/pkg/jwt"
)

const (
	// StateCookieName holds the signed CSRF state + OIDC nonce between the login
	// redirect and the callback. It is short-lived and scoped to the SSO paths.
	StateCookieName = "sso_state"
	stateTTL        = 10 * time.Minute
)

// State is the data carried across the OIDC authorization-code round-trip,
// stored in a signed (HS256, server JWT secret) cookie rather than server-side
// session storage — consistent with the stateless account_token model.
type State struct {
	State        string
	Nonce        string
	ProviderID   string
	OrgID        string
	ProviderSlug string
	Redirect     string
}

// RandomToken returns a URL-safe, 256-bit random string for state/nonce values.
func RandomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// SetStateCookie signs the state and writes it as a short-lived cookie.
func SetStateCookie(w http.ResponseWriter, r *http.Request, signer *jwt.Signer, s State) error {
	token, err := signer.GenerateWithClaims(stateTTL, map[string]string{
		"st":   s.State,
		"no":   s.Nonce,
		"pid":  s.ProviderID,
		"org":  s.OrgID,
		"slug": s.ProviderSlug,
		"rdr":  s.Redirect,
	})
	if err != nil {
		return err
	}

	http.SetCookie(w, &http.Cookie{
		Name:     StateCookieName,
		Value:    token,
		Path:     "/auth/sso/",
		MaxAge:   int(stateTTL.Seconds()),
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

// ReadStateCookie validates the signed state cookie and returns its contents.
func ReadStateCookie(r *http.Request, signer *jwt.Signer) (*State, error) {
	cookie, err := r.Cookie(StateCookieName)
	if err != nil {
		return nil, err
	}

	claims, err := signer.ValidateAndGetClaims(cookie.Value)
	if err != nil {
		return nil, err
	}

	get := func(k string) string {
		v, _ := claims[k].(string)
		return v
	}

	return &State{
		State:        get("st"),
		Nonce:        get("no"),
		ProviderID:   get("pid"),
		OrgID:        get("org"),
		ProviderSlug: get("slug"),
		Redirect:     get("rdr"),
	}, nil
}

// ClearStateCookie expires the state cookie after the callback completes.
func ClearStateCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     StateCookieName,
		Value:    "",
		Path:     "/auth/sso/",
		MaxAge:   -1,
		HttpOnly: true,
	})
}
