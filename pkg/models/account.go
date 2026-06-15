package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/superplanehq/superplane/pkg/database"
	"github.com/superplanehq/superplane/pkg/utils"
	"gorm.io/gorm"
)

type Account struct {
	ID                uuid.UUID `gorm:"primary_key;default:uuid_generate_v4()"`
	Email             string
	Name              string
	InstallationAdmin bool `gorm:"default:false"`
	PasswordChangedAt *time.Time
	DeactivatedAt     *time.Time
	CreatedAt         *time.Time
	UpdatedAt         *time.Time
}

func (a *Account) IsInstallationAdmin() bool {
	return a.InstallationAdmin
}

// IsDeactivated reports whether the account has been disabled. A deactivated
// account is rejected at login and on every authenticated request, and cannot
// be revived by an SSO login — it is a reversible "disabled" state, not a delete.
func (a *Account) IsDeactivated() bool {
	return a.DeactivatedAt != nil
}

// IsSessionFresh reports whether a token issued at the given Unix timestamp
// is still valid relative to the most recent password change. Tokens issued
// strictly before PasswordChangedAt are considered stale.
func (a *Account) IsSessionFresh(issuedAt int64) bool {
	if a.PasswordChangedAt == nil {
		return true
	}

	return issuedAt >= a.PasswordChangedAt.Unix()
}

// MarkPasswordChangedInTransaction stamps the password rotation time on the
// account. Used together with rotating the password hash and clearing API
// tokens to invalidate every existing session for the account.
func (a *Account) MarkPasswordChangedInTransaction(tx *gorm.DB, now time.Time) error {
	err := tx.Model(a).Update("password_changed_at", now).Error
	if err != nil {
		return err
	}

	a.PasswordChangedAt = &now
	return nil
}

func PromoteToInstallationAdmin(accountID string) error {
	return database.Conn().
		Model(&Account{}).
		Where("id = ?", accountID).
		Update("installation_admin", true).
		Error
}

func DemoteFromInstallationAdmin(accountID string) error {
	return database.Conn().
		Model(&Account{}).
		Where("id = ?", accountID).
		Update("installation_admin", false).
		Error
}

func Deactivate(accountID string, now time.Time) error {
	return DeactivateInTransaction(database.Conn(), accountID, now)
}

// DeactivateInTransaction disables the account (reversible; not a delete).
// Idempotent — re-deactivating an already-disabled account is a no-op and
// preserves the original timestamp.
func DeactivateInTransaction(tx *gorm.DB, accountID string, now time.Time) error {
	return tx.Model(&Account{}).
		Where("id = ? AND deactivated_at IS NULL", accountID).
		Update("deactivated_at", now).
		Error
}

func Reactivate(accountID string) error {
	return ReactivateInTransaction(database.Conn(), accountID)
}

func ReactivateInTransaction(tx *gorm.DB, accountID string) error {
	return tx.Model(&Account{}).
		Where("id = ?", accountID).
		Update("deactivated_at", nil).
		Error
}

// FindActivePasswordOnlyAccounts returns active (non-deactivated) accounts that
// can sign in only with a password — they have a password credential and no
// external identity link, so they would be stranded if password login were
// disabled. The excludeID account (the admin performing the change) is omitted.
func FindActivePasswordOnlyAccounts(excludeID string) ([]Account, error) {
	accounts := []Account{}
	err := database.Conn().
		Where("deactivated_at IS NULL").
		Where("id <> ?", excludeID).
		Where("EXISTS (SELECT 1 FROM account_password_auth pa WHERE pa.account_id = accounts.id)").
		Where("NOT EXISTS (SELECT 1 FROM account_providers ap WHERE ap.account_id = accounts.id)").
		Order("email").
		Find(&accounts).
		Error
	if err != nil {
		return nil, err
	}

	return accounts, nil
}

func CreateAccount(name, email string) (*Account, error) {
	return CreateAccountInTransaction(database.Conn(), name, email)
}

func CreateAccountInTransaction(tx *gorm.DB, name, email string) (*Account, error) {
	account := &Account{Name: name, Email: utils.NormalizeEmail(email)}
	err := tx.Create(account).Error
	if err != nil {
		return nil, err
	}

	return account, nil
}

func ListAccounts(search string, limit, offset int, sortBy, sortDirection string) ([]Account, int64, error) {
	query := database.Conn().Model(&Account{})

	if search != "" {
		query = query.Where("name ILIKE ? OR email ILIKE ?", "%"+search+"%", "%"+search+"%")
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if limit > 0 {
		query = query.Limit(limit)
	}

	if offset > 0 {
		query = query.Offset(offset)
	}

	orderClause := resolveOrderClause(sortBy, sortDirection, []string{"created_at", "name", "email"}, "created_at DESC")

	var accounts []Account
	if err := query.Order(orderClause).Find(&accounts).Error; err != nil {
		return nil, 0, err
	}

	return accounts, total, nil
}

func FindAccountByID(id string) (*Account, error) {
	var account Account

	err := database.Conn().
		Where("id = ?", id).
		First(&account).
		Error

	if err != nil {
		return nil, err
	}

	return &account, nil
}

func FindAccountByEmail(email string) (*Account, error) {
	var account Account

	err := database.Conn().
		Where("email = ?", utils.NormalizeEmail(email)).
		First(&account).
		Error

	if err != nil {
		return nil, err
	}

	return &account, nil
}

func (a *Account) GetAccountProviders() ([]AccountProvider, error) {
	providers := []AccountProvider{}

	err := database.Conn().
		Where("account_id = ?", a.ID).
		Find(&providers).
		Error

	if err != nil {
		return nil, err
	}

	return providers, nil
}

// CanLoginWithoutPassword reports whether the account has at least one external
// identity link (SSO/OAuth). Such an account can still authenticate when
// email/password login is disabled installation-wide; a password-only account
// cannot. Used to prevent an admin from locking themselves out.
func (a *Account) CanLoginWithoutPassword() (bool, error) {
	providers, err := a.GetAccountProviders()
	if err != nil {
		return false, err
	}

	return len(providers) > 0, nil
}

func (a *Account) GetAccountProvider(provider string) (*AccountProvider, error) {
	var account AccountProvider
	err := database.Conn().
		Where("account_id = ?", a.ID, provider).
		Where("provider = ?", provider).
		First(&account).
		Error

	if err != nil {
		return nil, err
	}

	return &account, err
}

func (a *Account) FindAccountProviderByID(provider, providerID string) (*AccountProvider, error) {
	var account AccountProvider

	err := database.Conn().
		Where("account_id = ?", a.ID).
		Where("provider = ?", provider).
		Where("provider_id = ?", providerID).
		First(&account).
		Error

	if err != nil {
		return nil, err
	}

	return &account, nil
}

func (a *Account) FindPendingInvitations() ([]OrganizationInvitation, error) {
	invitations := []OrganizationInvitation{}

	err := database.Conn().
		Where("email = ?", a.Email).
		Where("state = ?", InvitationStatePending).
		Find(&invitations).
		Error

	if err != nil {
		return nil, err
	}

	return invitations, nil
}

func FindAccountByProvider(provider, providerID string) (*Account, error) {
	var accountProvider AccountProvider
	err := database.Conn().
		Where("provider = ?", provider).
		Where("provider_id = ?", providerID).
		First(&accountProvider).
		Error

	if err != nil {
		return nil, err
	}

	return FindAccountByID(accountProvider.AccountID.String())
}

func (a *Account) UpdateEmail(newEmail string) error {
	normalizedEmail := utils.NormalizeEmail(newEmail)
	originalEmail := a.Email

	err := database.Conn().Transaction(func(tx *gorm.DB) error {
		err := tx.Model(a).Update("email", normalizedEmail).Error
		if err != nil {
			return err
		}

		err = tx.Model(&User{}).
			Where("account_id = ?", a.ID).
			Update("email", normalizedEmail).Error
		if err != nil {
			return err
		}

		return nil
	})

	if err == nil {
		a.Email = normalizedEmail
		return nil
	}

	a.Email = originalEmail
	return err
}

func (a *Account) UpdateEmailForProvider(newEmail, provider, providerID string) error {
	normalizedEmail := utils.NormalizeEmail(newEmail)

	err := database.Conn().Transaction(func(tx *gorm.DB) error {

		err := tx.Model(a).Update("email", normalizedEmail).Error
		if err != nil {
			return err
		}

		err = tx.Model(&User{}).
			Where("account_id = ?", a.ID).
			Update("email", normalizedEmail).Error
		if err != nil {
			return err
		}

		err = tx.Model(&AccountProvider{}).
			Where("account_id = ? AND provider = ? AND provider_id = ?", a.ID, provider, providerID).
			Update("email", normalizedEmail).Error
		if err != nil {
			return err
		}

		return nil
	})

	if err == nil {
		a.Email = normalizedEmail
	}

	return err
}
