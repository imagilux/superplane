package netguard

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestIsBlockedDialIP(t *testing.T) {
	orig := testAllowLoopback.Load()
	defer testAllowLoopback.Store(orig)
	testAllowLoopback.Store(false)

	// Always blocked, regardless of the per-client loopback policy.
	alwaysBlocked := []string{
		"169.254.169.254", "fe80::1", // link-local incl. cloud metadata
		"0.0.0.0", "::", // unspecified
		"224.0.0.1", "ff02::1", // multicast
	}
	for _, s := range alwaysBlocked {
		assert.True(t, isBlockedDialIP(net.ParseIP(s), false), "%s should be blocked", s)
		assert.True(t, isBlockedDialIP(net.ParseIP(s), true), "%s should stay blocked even when loopback is allowed", s)
	}

	// Loopback: blocked by the strict client, allowed when the client opts in
	// (the custom-agent-endpoint case, where a local model server is legitimate).
	for _, s := range []string{"127.0.0.1", "::1"} {
		assert.True(t, isBlockedDialIP(net.ParseIP(s), false), "%s should be blocked by the strict client", s)
		assert.False(t, isBlockedDialIP(net.ParseIP(s), true), "%s should be allowed by the loopback-friendly client", s)
	}

	// RFC1918 private and public addresses are always allowed — self-hosted
	// backends (e.g. our Authelia / LAN LLM at 192.168.1.61) must remain reachable.
	for _, s := range []string{"192.168.1.61", "10.0.0.5", "172.16.3.4", "8.8.8.8"} {
		assert.False(t, isBlockedDialIP(net.ParseIP(s), false), "%s should be allowed", s)
	}

	// The test override forces loopback allowed even for the strict client.
	testAllowLoopback.Store(true)
	assert.False(t, isBlockedDialIP(net.ParseIP("127.0.0.1"), false))
}

func TestGuardedClientCapsRedirects(t *testing.T) {
	orig := testAllowLoopback.Load()
	defer testAllowLoopback.Store(orig)
	testAllowLoopback.Store(true) // the httptest server listens on loopback

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/loop", http.StatusFound)
	}))
	defer srv.Close()

	resp, err := NewGuardedHTTPClient(2 * time.Second).Get(srv.URL)
	if resp != nil {
		resp.Body.Close()
	}
	assert.Error(t, err, "a redirect loop must be capped, not followed indefinitely")
}
