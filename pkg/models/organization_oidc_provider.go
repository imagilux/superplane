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
	AllowedGroups       datatypes.JSONSlice[string]
	GroupRoleMappings   datatypes.JSONType[map[string]string]
	GroupsClaim         string
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

	p := &OrganizationOIDCProvider{
		ID:             uuid.New(),
		OrganizationID: orgID,
		Slug:           slug,
		DisplayName:    displayName,
		Type:           providerType,
		IssuerURL:      issuerURL,
		ClientID:       clientID,
		Enabled:        enabled,
		CreatedBy:      createdBy,
	}
	p.SetScopes(scopes)
	p.SetAllowedEmailDomains(allowedDomains)
	return p
}

// SetScopes sets the requested scopes, falling back to DefaultOIDCScopes when none
// are provided (an OIDC flow always needs at least "openid").
func (p *OrganizationOIDCProvider) SetScopes(scopes []string) {
	if len(scopes) == 0 {
		scopes = append([]string{}, DefaultOIDCScopes...)
	}
	p.Scopes = datatypes.JSONSlice[string](scopes)
}

// SetAllowedEmailDomains normalizes (lower-cases, trims, drops empties) and stores
// the allowed email domains. An empty result means no domain restriction.
func (p *OrganizationOIDCProvider) SetAllowedEmailDomains(domains []string) {
	p.AllowedEmailDomains = datatypes.JSONSlice[string](normalizeDomains(domains))
}

// SetAllowedGroups stores the IdP groups permitted to use this provider (trimmed,
// empties dropped). Group names are case-sensitive. Empty means no restriction.
func (p *OrganizationOIDCProvider) SetAllowedGroups(groups []string) {
	p.AllowedGroups = datatypes.JSONSlice[string](normalizeStrings(groups))
}

// SetGroupRoleMappings stores the IdP group -> org role map (blank entries dropped).
func (p *OrganizationOIDCProvider) SetGroupRoleMappings(m map[string]string) {
	clean := map[string]string{}
	for group, role := range m {
		group = strings.TrimSpace(group)
		if group != "" && strings.TrimSpace(role) != "" {
			clean[group] = strings.TrimSpace(role)
		}
	}
	p.GroupRoleMappings = datatypes.NewJSONType(clean)
}

// AllowsGroups reports whether a user with the given IdP groups may use this
// provider. A provider with no configured groups imposes no restriction.
func (p *OrganizationOIDCProvider) AllowsGroups(groups []string) bool {
	if len(p.AllowedGroups) == 0 {
		return true
	}
	allowed := make(map[string]struct{}, len(p.AllowedGroups))
	for _, g := range p.AllowedGroups {
		allowed[g] = struct{}{}
	}
	for _, g := range groups {
		if _, ok := allowed[g]; ok {
			return true
		}
	}
	return false
}

// ResolveRole returns the highest-precedence org role mapped from the user's
// groups, or "" when no mapping is configured or no group matches.
func (p *OrganizationOIDCProvider) ResolveRole(groups []string) string {
	mappings := p.GroupRoleMappings.Data()
	if len(mappings) == 0 {
		return ""
	}
	best, bestRank := "", 0
	for _, g := range groups {
		if role, ok := mappings[g]; ok {
			if r := orgRoleRank(role); r > bestRank {
				best, bestRank = role, r
			}
		}
	}
	return best
}

// HasGroupFeatures reports whether the provider uses the OIDC groups claim (to
// gate access or map roles), in which case the groups scope must be requested.
func (p *OrganizationOIDCProvider) HasGroupFeatures() bool {
	return len(p.AllowedGroups) > 0 || len(p.GroupRoleMappings.Data()) > 0
}

// GroupsClaimOrDefault returns the ID-token claim that group membership is read
// from, defaulting to "groups" when the provider hasn't overridden it. Set it
// for IdPs (e.g. Okta, Entra ID) that emit groups under a different claim name.
func (p *OrganizationOIDCProvider) GroupsClaimOrDefault() string {
	if p.GroupsClaim == "" {
		return "groups"
	}
	return p.GroupsClaim
}

// UsesDefaultGroupsClaim reports whether the provider reads groups from the
// conventional "groups" claim — used to decide whether to auto-request the
// "groups" scope (only safe for the default; a custom claim's scope is the
// admin's responsibility).
func (p *OrganizationOIDCProvider) UsesDefaultGroupsClaim() bool {
	return p.GroupsClaim == ""
}

// SetGroupsClaim sets the groups claim name (trimmed); empty restores the
// default "groups".
func (p *OrganizationOIDCProvider) SetGroupsClaim(claim string) {
	p.GroupsClaim = strings.TrimSpace(claim)
}

func orgRoleRank(role string) int {
	switch role {
	case RoleOrgOwner:
		return 3
	case RoleOrgAdmin:
		return 2
	case RoleOrgViewer:
		return 1
	default:
		return 0
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

// ExistsEnabledOIDCProvider reports whether any organization has an enabled OIDC
// provider. Used to decide whether to surface SSO on the login screen.
func ExistsEnabledOIDCProvider() (bool, error) {
	var count int64
	err := database.Conn().
		Model(&OrganizationOIDCProvider{}).
		Where("enabled = ?", true).
		Count(&count).
		Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// SoleEnabledOIDCProvider returns the single enabled OIDC provider when exactly
// one exists across all organizations, or (nil, nil) otherwise. Unattended
// auto-login uses it: it only fires when there is one unambiguous provider to
// target (no discovery email is available on a fresh page load).
func SoleEnabledOIDCProvider() (*OrganizationOIDCProvider, error) {
	var providers []OrganizationOIDCProvider
	err := database.Conn().
		Where("enabled = ?", true).
		Limit(2).
		Find(&providers).
		Error
	if err != nil {
		return nil, err
	}
	if len(providers) != 1 {
		return nil, nil
	}
	return &providers[0], nil
}

// normalizeStrings trims whitespace and drops empty entries (case preserved).
func normalizeStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v != "" {
			out = append(out, v)
		}
	}
	return out
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
