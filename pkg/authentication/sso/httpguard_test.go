package sso

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsBlockedDialIP(t *testing.T) {
	orig := allowLoopback.Load()
	defer allowLoopback.Store(orig)

	allowLoopback.Store(false)

	blocked := []string{
		"127.0.0.1", "::1", // loopback
		"169.254.169.254", "fe80::1", // link-local incl. cloud metadata
		"0.0.0.0", "::", // unspecified
		"224.0.0.1", "ff02::1", // multicast
	}
	for _, s := range blocked {
		assert.True(t, isBlockedDialIP(net.ParseIP(s)), "%s should be blocked", s)
	}

	// RFC1918 private and public addresses are allowed — self-hosted IdPs
	// (e.g. our Authelia at 192.168.1.61) must remain reachable.
	allowed := []string{"192.168.1.61", "10.0.0.5", "172.16.3.4", "8.8.8.8"}
	for _, s := range allowed {
		assert.False(t, isBlockedDialIP(net.ParseIP(s)), "%s should be allowed", s)
	}

	// Loopback is permitted only when explicitly enabled for tests.
	allowLoopback.Store(true)
	assert.False(t, isBlockedDialIP(net.ParseIP("127.0.0.1")))
}
