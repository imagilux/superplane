// Package netguard provides SSRF-resistant HTTP clients for server-side fetches
// to operator- or admin-supplied URLs — OIDC issuer discovery / JWKS and custom
// OpenAI-compatible agent endpoints. The dialer resolves the target host and
// refuses blocked address ranges, re-validating at dial time so a DNS rebind
// between resolution and connection cannot reach a blocked address.
package netguard

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync/atomic"
	"time"
)

// testAllowLoopback, when set, permits loopback targets for every guarded client
// in the process regardless of its per-client policy. TEST USE ONLY — the real
// loopback policy is chosen per client (see the constructors below); production
// never calls AllowLoopbackForTesting.
var testAllowLoopback atomic.Bool

// AllowLoopbackForTesting permits loopback targets for every guarded client in
// the process. TEST USE ONLY; never call from production code paths. Tests'
// in-memory httptest servers listen on 127.0.0.1.
func AllowLoopbackForTesting() { testAllowLoopback.Store(true) }

// isBlockedDialIP reports whether ip must not be dialed for an outbound fetch to
// an admin-supplied URL. Link-local (which includes the cloud metadata address
// 169.254.169.254), unspecified, and multicast are ALWAYS blocked — the
// highest-value SSRF targets. Loopback (the app's own internal ports) is blocked
// unless the caller opts in via allowLoopback, which only the local-endpoint
// client does, because a model server on 127.0.0.1 is a legitimate target there.
// RFC1918 private ranges are intentionally allowed, because self-hosted backends
// (Authelia, Keycloak, a LAN LLM, …) commonly live on private networks.
func isBlockedDialIP(ip net.IP, allowLoopback bool) bool {
	if ip.IsLoopback() {
		return !(allowLoopback || testAllowLoopback.Load())
	}
	return ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsInterfaceLocalMulticast()
}

// NewGuardedHTTPClient returns an SSRF-resistant http.Client that refuses
// loopback, link-local (cloud metadata), unspecified, and multicast targets.
// Use it for admin-supplied URLs that have no legitimate loopback target — e.g.
// OIDC issuer discovery, where the IdP is never the app's own loopback.
func NewGuardedHTTPClient(timeout time.Duration) *http.Client {
	return newGuardedClient(timeout, false)
}

// NewGuardedHTTPClientAllowingLoopback is like NewGuardedHTTPClient but also
// permits loopback targets, for admin-supplied endpoints where a server on
// 127.0.0.1 is a first-class target — a local model server (Ollama, llama.cpp)
// behind a custom agent provider. Link-local (cloud metadata), unspecified, and
// multicast remain blocked.
func NewGuardedHTTPClientAllowingLoopback(timeout time.Duration) *http.Client {
	return newGuardedClient(timeout, true)
}

// newGuardedClient builds the guarded client. allowLoopback selects whether
// loopback targets are permitted (see the two exported constructors).
func newGuardedClient(timeout time.Duration, allowLoopback bool) *http.Client {
	dialer := &net.Dialer{Timeout: timeout}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}

			ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, err
			}

			for _, ip := range ips {
				if isBlockedDialIP(ip.IP, allowLoopback) {
					return nil, fmt.Errorf("refusing to connect to blocked address for host %q", host)
				}
			}

			// Dial a validated, pinned IP so a rebind cannot swap in a blocked one.
			var lastErr error
			for _, ip := range ips {
				conn, derr := dialer.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
				if derr == nil {
					return conn, nil
				}
				lastErr = derr
			}
			return nil, lastErr
		},
		TLSHandshakeTimeout:   timeout,
		ResponseHeaderTimeout: timeout,
	}

	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return fmt.Errorf("stopped after too many redirects")
			}
			return nil
		},
	}
}
