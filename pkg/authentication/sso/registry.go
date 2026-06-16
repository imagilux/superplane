// Package sso implements the per-organization generic OIDC Single Sign-On engine:
// dynamic provider discovery/verification (decoupled from goth's global registry)
// and a stateless, signed state+nonce cookie for the authorization-code flow.
package sso

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// Config describes one OIDC provider to the registry. It is intentionally
// decoupled from the database model so the engine stays independently testable.
type Config struct {
	ID           string
	IssuerURL    string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	Scopes       []string
}

type entry struct {
	oauthConfig *oauth2.Config
	verifier    *oidc.IDTokenVerifier
	expiresAt   time.Time
}

// Registry caches OIDC discovery results (provider metadata + verifier) per
// provider configuration, with a TTL. Discovery is a network round-trip to the
// IdP, so caching avoids hitting it on every login.
type Registry struct {
	mu         sync.RWMutex
	entries    map[string]*entry
	ttl        time.Duration
	httpClient *http.Client
}

func NewRegistry(ttl time.Duration) *Registry {
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	return &Registry{
		entries:    make(map[string]*entry),
		ttl:        ttl,
		httpClient: NewGuardedHTTPClient(10 * time.Second),
	}
}

// ClientContext returns a context whose HTTP client (used by both go-oidc and
// golang.org/x/oauth2) is the registry's bounded-timeout client.
func (r *Registry) ClientContext(ctx context.Context) context.Context {
	return oidc.ClientContext(ctx, r.httpClient)
}

func cacheKey(c Config) string {
	// Include a fingerprint of the client secret so rotating it (which leaves
	// issuer/clientID/redirect unchanged) yields a new key — otherwise the stale
	// secret would be served from cache until the TTL expires.
	fp := sha256.Sum256([]byte(c.ClientSecret))
	return strings.Join([]string{c.ID, c.IssuerURL, c.ClientID, c.RedirectURL, fmt.Sprintf("%x", fp[:8])}, "|")
}

// Get returns a ready oauth2 config and ID-token verifier for the provider,
// performing OIDC discovery on a cache miss. Discovery errors are returned (and
// NOT cached) so a temporarily-unreachable IdP cannot poison the cache or break
// other login methods.
func (r *Registry) Get(ctx context.Context, c Config) (*oauth2.Config, *oidc.IDTokenVerifier, error) {
	key := cacheKey(c)

	r.mu.RLock()
	if e, ok := r.entries[key]; ok && time.Now().Before(e.expiresAt) {
		r.mu.RUnlock()
		return e.oauthConfig, e.verifier, nil
	}
	r.mu.RUnlock()

	provider, err := oidc.NewProvider(r.ClientContext(ctx), c.IssuerURL)
	if err != nil {
		return nil, nil, fmt.Errorf("oidc discovery failed for issuer %q: %w", c.IssuerURL, err)
	}

	scopes := c.Scopes
	if len(scopes) == 0 {
		scopes = []string{oidc.ScopeOpenID, "email", "profile"}
	}

	oauthConfig := &oauth2.Config{
		ClientID:     c.ClientID,
		ClientSecret: c.ClientSecret,
		Endpoint:     provider.Endpoint(),
		RedirectURL:  c.RedirectURL,
		Scopes:       scopes,
	}
	verifier := provider.Verifier(&oidc.Config{ClientID: c.ClientID})

	r.mu.Lock()
	r.entries[key] = &entry{
		oauthConfig: oauthConfig,
		verifier:    verifier,
		expiresAt:   time.Now().Add(r.ttl),
	}
	r.mu.Unlock()

	return oauthConfig, verifier, nil
}

// Invalidate drops all cached entries for a provider id. Best-effort and
// process-local; multi-instance deployments converge via the TTL.
func (r *Registry) Invalidate(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for k := range r.entries {
		if strings.HasPrefix(k, id+"|") {
			delete(r.entries, k)
		}
	}
}
