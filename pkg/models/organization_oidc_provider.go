package models

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	uuid "github.com/google/uuid"
	"github.com/superplanehq/superplane/pkg/crypto"
	"github.com/superplanehq/superplane/pkg/database"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// OrganizationOIDCProvider is a per-organization, admin-configured generic OIDC
// identity provider used for Single Sign-On. An organization may have multiple
// providers. The client secret is stored encrypted at rest (base64 of AES-GCM
// ciphertext) and bound to the row via the provider ID as associated data, so a
// ciphertext cannot be replayed against a different row.
type OrganizationOIDCProvider struct {
	ID                  uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	OrganizationID      uuid.UUID
	Slug                string
	DisplayName         string
	Type                string
	IssuerURL           string
	ClientID            string
	ClientSecretEnc     string
	Scopes              datatypes.JSONSlice[string]
	AllowedEmailDomains datatypes.JSONSlice[string]
	Enabled             bool
	CreatedBy           *uuid.UUID
	CreatedAt           *time.Time
	UpdatedAt           *time.Time
	DeletedAt           gorm.DeletedAt `gorm:"index"`
}

// TableName pins the table name; GORM's default namer would mangle "OIDC" into
// "o_id_c", which would not match the migration's organization_oidc_providers.
func (OrganizationOIDCProvider) TableName() string {
	return "organization_oidc_providers"
}

// DefaultOIDCScopes are applied when an admin does not specify scopes.
var DefaultOIDCScopes = []string{"openid", "email", "profile"}

// NewOIDCProvider builds an in-memory provider with a freshly generated ID so the
// client secret can be encrypted (the ID is the encryption associated data) before
// the row is persisted. The secret is set separately via SetClientSecret.
func NewOIDCProvider(orgID uuid.UUID, createdBy *uuid.UUID, slug, displayName, providerType, issuerURL, clientID string, scopes, allowedDomains []string, enabled bool) *OrganizationOIDCProvider {
	if providerType == "" {
		providerType = OIDCProviderTypeOIDC
	}
	if len(scopes) == 0 {
		scopes = append([]string{}, DefaultOIDCScopes...)
	}
	if allowedDomains == nil {
		allowedDomains = []string{}
	}

	return &OrganizationOIDCProvider{
		ID:                  uuid.New(),
		OrganizationID:      orgID,
		Slug:                slug,
		DisplayName:         displayName,
		Type:                providerType,
		IssuerURL:           issuerURL,
		ClientID:            clientID,
		Scopes:              datatypes.JSONSlice[string](scopes),
		AllowedEmailDomains: datatypes.JSONSlice[string](normalizeDomains(allowedDomains)),
		Enabled:             enabled,
		CreatedBy:           createdBy,
	}
}

// SetClientSecret encrypts and stores the client secret. The provider ID must be
// set (NewOIDCProvider guarantees this) before calling.
func (p *OrganizationOIDCProvider) SetClientSecret(ctx context.Context, enc crypto.Encryptor, plaintext string) error {
	ciphertext, err := enc.Encrypt(ctx, []byte(plaintext), []byte(p.ID.String()))
	if err != nil {
		return err
	}

	p.ClientSecretEnc = base64.StdEncoding.EncodeToString(ciphertext)
	return nil
}

// DecryptClientSecret returns the plaintext client secret.
func (p *OrganizationOIDCProvider) DecryptClientSecret(ctx context.Context, enc crypto.Encryptor) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(p.ClientSecretEnc)
	if err != nil {
		return "", err
	}

	plaintext, err := enc.Decrypt(ctx, raw, []byte(p.ID.String()))
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}

// HasClientSecret reports whether a secret is stored, without exposing it.
func (p *OrganizationOIDCProvider) HasClientSecret() bool {
	return p.ClientSecretEnc != ""
}

// AllowsEmailDomain reports whether the given email domain may use this provider.
// A provider with no configured domains imposes no restriction.
func (p *OrganizationOIDCProvider) AllowsEmailDomain(domain string) bool {
	if len(p.AllowedEmailDomains) == 0 {
		return true
	}

	domain = strings.ToLower(strings.TrimSpace(domain))
	for _, d := range p.AllowedEmailDomains {
		if strings.ToLower(strings.TrimSpace(d)) == domain {
			return true
		}
	}

	return false
}

func (p *OrganizationOIDCProvider) Create() error {
	err := database.Conn().Clauses(clause.Returning{}).Create(p).Error
	if err != nil && strings.Contains(err.Error(), "duplicate key value violates unique constraint") {
		return ErrNameAlreadyUsed
	}
	return err
}

func (p *OrganizationOIDCProvider) Save() error {
	now := time.Now()
	p.UpdatedAt = &now

	err := database.Conn().Clauses(clause.Returning{}).Save(p).Error
	if err != nil && strings.Contains(err.Error(), "duplicate key value violates unique constraint") {
		return ErrNameAlreadyUsed
	}
	return err
}

func (p *OrganizationOIDCProvider) Delete() error {
	return database.Conn().Delete(p).Error
}

func FindOIDCProvidersByOrganization(orgID uuid.UUID) ([]OrganizationOIDCProvider, error) {
	var providers []OrganizationOIDCProvider

	err := database.Conn().
		Where("organization_id = ?", orgID).
		Order("created_at asc").
		Find(&providers).
		Error

	if err != nil {
		return nil, err
	}

	return providers, nil
}

func FindOIDCProviderByID(orgID, id uuid.UUID) (*OrganizationOIDCProvider, error) {
	var provider OrganizationOIDCProvider

	err := database.Conn().
		Where("organization_id = ?", orgID).
		Where("id = ?", id).
		First(&provider).
		Error

	if err != nil {
		return nil, err
	}

	return &provider, nil
}

func FindOIDCProviderBySlug(orgID uuid.UUID, slug string) (*OrganizationOIDCProvider, error) {
	var provider OrganizationOIDCProvider

	err := database.Conn().
		Where("organization_id = ?", orgID).
		Where("slug = ?", slug).
		First(&provider).
		Error

	if err != nil {
		return nil, err
	}

	return &provider, nil
}

// FindEnabledOIDCProvidersByEmailDomain returns enabled providers across all
// organizations whose allowed_email_domains contains the given domain. Used for
// home-realm discovery; providers with no domain restriction are intentionally
// excluded (they are reachable only via a direct org login URL).
func FindEnabledOIDCProvidersByEmailDomain(domain string) ([]OrganizationOIDCProvider, error) {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" {
		return []OrganizationOIDCProvider{}, nil
	}

	var providers []OrganizationOIDCProvider
	err := database.Conn().
		Where("enabled = ?", true).
		Where("allowed_email_domains @> ?::jsonb", fmt.Sprintf("[%q]", domain)).
		Find(&providers).
		Error

	if err != nil {
		return nil, err
	}

	return providers, nil
}

func normalizeDomains(domains []string) []string {
	out := make([]string, 0, len(domains))
	for _, d := range domains {
		d = strings.ToLower(strings.TrimSpace(d))
		if d != "" {
			out = append(out, d)
		}
	}
	return out
}
