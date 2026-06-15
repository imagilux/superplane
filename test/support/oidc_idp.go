package support

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"time"

	jwtLib "github.com/golang-jwt/jwt/v4"
	"github.com/stretchr/testify/require"
	"github.com/superplanehq/superplane/pkg/authentication/sso"
)

// MockOIDCProvider is an in-memory OIDC identity provider for tests. It serves
// discovery, JWKS, authorization, and token endpoints, signing ID tokens with a
// freshly generated RSA key. Tests register the claims to return per auth code.
type MockOIDCProvider struct {
	Server   *httptest.Server
	Issuer   string
	ClientID string

	key   *rsa.PrivateKey
	kid   string
	mu    sync.Mutex
	codes map[string]MockIDClaims
}

// MockIDClaims are the ID-token claims the mock issues for a given auth code.
type MockIDClaims struct {
	Sub           string
	Email         string
	Name          string
	Nonce         string
	EmailVerified bool
}

// NewMockOIDCProvider starts a mock IdP and registers cleanup on the test.
func NewMockOIDCProvider(t require.TestingT, clientID string) *MockOIDCProvider {
	// The mock listens on loopback (httptest); allow the SSRF guard to reach it.
	sso.AllowLoopbackForTesting()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	m := &MockOIDCProvider{
		ClientID: clientID,
		key:      key,
		kid:      "mock-key-1",
		codes:    map[string]MockIDClaims{},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", m.handleDiscovery)
	mux.HandleFunc("/jwks", m.handleJWKS)
	mux.HandleFunc("/authorize", m.handleAuthorize)
	mux.HandleFunc("/token", m.handleToken)

	m.Server = httptest.NewServer(mux)
	m.Issuer = m.Server.URL

	if tt, ok := t.(interface{ Cleanup(func()) }); ok {
		tt.Cleanup(m.Server.Close)
	}
	return m
}

// RegisterCode tells the mock which ID-token claims to mint when its token
// endpoint is called with the given authorization code.
func (m *MockOIDCProvider) RegisterCode(code string, claims MockIDClaims) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.codes[code] = claims
}

func (m *MockOIDCProvider) handleDiscovery(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{
		"issuer":                                m.Issuer,
		"authorization_endpoint":                m.Issuer + "/authorize",
		"token_endpoint":                        m.Issuer + "/token",
		"jwks_uri":                              m.Issuer + "/jwks",
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"scopes_supported":                      []string{"openid", "email", "profile"},
		"claims_supported":                      []string{"sub", "email", "email_verified", "name", "preferred_username"},
	})
}

func (m *MockOIDCProvider) handleJWKS(w http.ResponseWriter, _ *http.Request) {
	pub := m.key.Public().(*rsa.PublicKey)
	writeJSON(w, map[string]any{
		"keys": []map[string]any{{
			"kty": "RSA",
			"use": "sig",
			"alg": "RS256",
			"kid": m.kid,
			"n":   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
			"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
		}},
	})
}

// handleAuthorize simulates user approval by redirecting straight back to the
// client's redirect_uri with a code, recording the request nonce against it.
func (m *MockOIDCProvider) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	redirectURI := q.Get("redirect_uri")
	code := "mock-code-" + q.Get("state")
	m.RegisterCode(code, MockIDClaims{Sub: "mock-sub", Email: "user@example.com", Nonce: q.Get("nonce"), EmailVerified: true})

	u, _ := url.Parse(redirectURI)
	rq := u.Query()
	rq.Set("code", code)
	rq.Set("state", q.Get("state"))
	u.RawQuery = rq.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}

func (m *MockOIDCProvider) handleToken(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	code := r.FormValue("code")

	m.mu.Lock()
	claims, ok := m.codes[code]
	m.mu.Unlock()
	if !ok {
		http.Error(w, "invalid code", http.StatusBadRequest)
		return
	}

	now := time.Now()
	token := jwtLib.NewWithClaims(jwtLib.SigningMethodRS256, jwtLib.MapClaims{
		"iss":            m.Issuer,
		"aud":            m.ClientID,
		"sub":            claims.Sub,
		"exp":            now.Add(time.Hour).Unix(),
		"iat":            now.Unix(),
		"nonce":          claims.Nonce,
		"email":          claims.Email,
		"email_verified": claims.EmailVerified,
		"name":           claims.Name,
	})
	token.Header["kid"] = m.kid
	signed, err := token.SignedString(m.key)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]any{
		"access_token": "mock-access-token",
		"token_type":   "Bearer",
		"expires_in":   3600,
		"id_token":     signed,
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
