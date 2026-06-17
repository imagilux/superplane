package sso

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync/atomic"
	"time"
)

// allowLoopback permits dialing loopback targets. It is false in production —
// loopback is a high-value SSRF target (the app's own internal ports) — and is
// set true only by tests, whose in-memory httptest IdP listens on 127.0.0.1.
var allowLoopback atomic.Bool

// AllowLoopbackForTesting permits loopback discovery/JWKS targets. TEST USE ONLY;
// never call this from production code paths.
func AllowLoopbackForTesting() { allowLoopback.Store(true) }

// isBlockedDialIP reports whether an IP must not be dialed for outbound OIDC
// discovery / JWKS fetches. We block the highest-value SSRF targets — loopback,
// link-local (which includes the cloud metadata address 169.254.169.254),
// unspecified, and multicast — but intentionally ALLOW RFC1918 private ranges,
// because self-hosted identity providers (Authelia, Keycloak, …) commonly live
// on private networks, which is the entire point of generic OIDC SSO.
func isBlockedDialIP(ip net.IP) bool {
	if ip.IsLoopback() {
		return !allowLoopback.Load()
	}
	return ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsInterfaceLocalMulticast()
}

// NewGuardedHTTPClient returns an http.Client whose dialer resolves the target
// host and refuses to connect to blocked IP ranges, re-validating at dial time
// so a DNS rebind between resolution and connection cannot reach a blocked
// address (the connection is pinned to a validated IP). Proxies are disabled so
// the guard always sees the real target, and redirects are capped.
func NewGuardedHTTPClient(timeout time.Duration) *http.Client {
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
				if isBlockedDialIP(ip.IP) {
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
