package sso

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/superplanehq/superplane/pkg/jwt"
)

func TestStateCookieRoundTrip(t *testing.T) {
	signer := jwt.NewSigner("test-secret")
	st := State{State: "abc", Nonce: "xyz", ProviderID: "pid", OrgID: "org", ProviderSlug: "idp", Redirect: "/dash"}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/auth/sso/org/idp", nil)
	require.NoError(t, SetStateCookie(rec, req, signer, st))

	cookie := rec.Result().Cookies()[0]
	assert.Equal(t, StateCookieName, cookie.Name)
	assert.True(t, cookie.HttpOnly)

	readReq := httptest.NewRequest("GET", "/auth/sso/org/idp/callback", nil)
	readReq.AddCookie(cookie)
	got, err := ReadStateCookie(readReq, signer)
	require.NoError(t, err)
	assert.Equal(t, st, *got)
}

func TestStateCookieTamperRejected(t *testing.T) {
	signer := jwt.NewSigner("test-secret")
	req := httptest.NewRequest("GET", "/cb", nil)
	req.AddCookie(&http.Cookie{Name: StateCookieName, Value: "tampered.value.here"})
	_, err := ReadStateCookie(req, signer)
	assert.Error(t, err)
}

func TestStateCookieWrongSecretRejected(t *testing.T) {
	signer := jwt.NewSigner("secret-a")
	other := jwt.NewSigner("secret-b")

	rec := httptest.NewRecorder()
	require.NoError(t, SetStateCookie(rec, httptest.NewRequest("GET", "/x", nil), signer, State{State: "s", Nonce: "n"}))
	cookie := rec.Result().Cookies()[0]

	readReq := httptest.NewRequest("GET", "/x", nil)
	readReq.AddCookie(cookie)
	_, err := ReadStateCookie(readReq, other)
	assert.Error(t, err, "a cookie signed with a different secret must not validate")
}

func TestRandomTokenUnique(t *testing.T) {
	a, err := RandomToken()
	require.NoError(t, err)
	b, err := RandomToken()
	require.NoError(t, err)
	assert.NotEmpty(t, a)
	assert.NotEqual(t, a, b)
}
