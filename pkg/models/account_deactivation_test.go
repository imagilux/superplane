package models

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/superplanehq/superplane/pkg/database"
)

func TestAccountDeactivation(t *testing.T) {
	require.NoError(t, database.TruncateTables())

	acc, err := CreateAccount("Pw Only", "pw@example.com")
	require.NoError(t, err)
	_, err = CreateAccountPasswordAuth(acc.ID, "hash")
	require.NoError(t, err)

	zeroID := "00000000-0000-0000-0000-000000000000"

	t.Run("deactivate / reactivate round-trips", func(t *testing.T) {
		assert.False(t, acc.IsDeactivated())

		require.NoError(t, Deactivate(acc.ID.String(), time.Now()))
		fresh, err := FindAccountByID(acc.ID.String())
		require.NoError(t, err)
		assert.True(t, fresh.IsDeactivated())

		require.NoError(t, Reactivate(acc.ID.String()))
		fresh, err = FindAccountByID(acc.ID.String())
		require.NoError(t, err)
		assert.False(t, fresh.IsDeactivated())
	})

	t.Run("FindActivePasswordOnlyAccounts filters correctly", func(t *testing.T) {
		// Active + password-only -> included.
		got, err := FindActivePasswordOnlyAccounts(zeroID)
		require.NoError(t, err)
		assert.True(t, containsEmail(got, "pw@example.com"))

		// An SSO-linked account is excluded (it can still log in).
		sso, err := CreateAccount("SSO", "sso@example.com")
		require.NoError(t, err)
		require.NoError(t, database.Conn().Create(&AccountProvider{
			AccountID: sso.ID, Provider: "oidc:x", ProviderID: "s", Email: sso.Email,
		}).Error)
		got, err = FindActivePasswordOnlyAccounts(zeroID)
		require.NoError(t, err)
		assert.False(t, containsEmail(got, "sso@example.com"))

		// Excluded by id (the acting admin).
		got, err = FindActivePasswordOnlyAccounts(acc.ID.String())
		require.NoError(t, err)
		assert.False(t, containsEmail(got, "pw@example.com"))

		// Already-deactivated accounts are excluded.
		require.NoError(t, Deactivate(acc.ID.String(), time.Now()))
		got, err = FindActivePasswordOnlyAccounts(zeroID)
		require.NoError(t, err)
		assert.False(t, containsEmail(got, "pw@example.com"))
	})
}

func containsEmail(accounts []Account, email string) bool {
	for _, a := range accounts {
		if a.Email == email {
			return true
		}
	}
	return false
}
