package models

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	uuid "github.com/google/uuid"
	"github.com/superplanehq/superplane/pkg/crypto"
	"github.com/superplanehq/superplane/pkg/database"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// OrganizationAgentProvider is a per-organization, admin-configured AI agent
// backend (an OpenAI-compatible endpoint). An organization may configure several;
// the enabled one is used for that org's agent sessions. The API key is stored
// encrypted at rest (base64 of AES-GCM ciphertext) and bound to the row via the
// provider ID as associated data, so a ciphertext cannot be replayed against a
// different row. The key is optional — unauthenticated local servers need none.
type OrganizationAgentProvider struct {
	ID             uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	OrganizationID uuid.UUID
	Slug           string
	DisplayName    string
	Type           string
	BaseURL        string
	Model          string
	APIKeyEnc      string
	Enabled        bool
	CreatedBy      *uuid.UUID
	CreatedAt      *time.Time
	UpdatedAt      *time.Time
	DeletedAt      gorm.DeletedAt `gorm:"index"`
}

// TableName pins the table name to match the migration.
func (OrganizationAgentProvider) TableName() string {
	return "organization_agent_providers"
}

// AgentProviderTypeOpenAI is the OpenAI-compatible provider kind — the only one
// configurable per-org today (the Anthropic managed-agent path stays
// installation-level).
const AgentProviderTypeOpenAI = "openai"

// NewAgentProvider builds an in-memory provider with a freshly generated ID so the
// API key can be encrypted (the ID is the encryption associated data) before the
// row is persisted. The key is set separately via SetAPIKey.
func NewAgentProvider(orgID uuid.UUID, createdBy *uuid.UUID, slug, displayName, providerType, baseURL, model string, enabled bool) *OrganizationAgentProvider {
	if providerType == "" {
		providerType = AgentProviderTypeOpenAI
	}

	return &OrganizationAgentProvider{
		ID:             uuid.New(),
		OrganizationID: orgID,
		Slug:           slug,
		DisplayName:    displayName,
		Type:           providerType,
		BaseURL:        baseURL,
		Model:          model,
		Enabled:        enabled,
		CreatedBy:      createdBy,
	}
}

// SetAPIKey encrypts and stores the API key, bound to the provider ID. An empty
// key clears it (unauthenticated local servers need none). The provider ID must
// be set (NewAgentProvider guarantees this) before calling.
func (p *OrganizationAgentProvider) SetAPIKey(ctx context.Context, enc crypto.Encryptor, plaintext string) error {
	if plaintext == "" {
		p.APIKeyEnc = ""
		return nil
	}

	ciphertext, err := enc.Encrypt(ctx, []byte(plaintext), []byte(p.ID.String()))
	if err != nil {
		return err
	}

	p.APIKeyEnc = base64.StdEncoding.EncodeToString(ciphertext)
	return nil
}

// DecryptAPIKey returns the plaintext API key, or "" when none is stored.
func (p *OrganizationAgentProvider) DecryptAPIKey(ctx context.Context, enc crypto.Encryptor) (string, error) {
	if p.APIKeyEnc == "" {
		return "", nil
	}

	raw, err := base64.StdEncoding.DecodeString(p.APIKeyEnc)
	if err != nil {
		return "", err
	}

	plaintext, err := enc.Decrypt(ctx, raw, []byte(p.ID.String()))
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}

// HasAPIKey reports whether an API key is stored, without exposing it.
func (p *OrganizationAgentProvider) HasAPIKey() bool {
	return p.APIKeyEnc != ""
}

func (p *OrganizationAgentProvider) Create() error {
	err := database.Conn().Clauses(clause.Returning{}).Create(p).Error
	if err != nil && strings.Contains(err.Error(), "duplicate key value violates unique constraint") {
		return ErrNameAlreadyUsed
	}
	return err
}

func (p *OrganizationAgentProvider) Save() error {
	now := time.Now()
	p.UpdatedAt = &now

	err := database.Conn().Clauses(clause.Returning{}).Save(p).Error
	if err != nil && strings.Contains(err.Error(), "duplicate key value violates unique constraint") {
		return ErrNameAlreadyUsed
	}
	return err
}

func (p *OrganizationAgentProvider) Delete() error {
	return database.Conn().Delete(p).Error
}

func FindAgentProvidersByOrganization(orgID uuid.UUID) ([]OrganizationAgentProvider, error) {
	var providers []OrganizationAgentProvider

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

func FindAgentProviderByID(orgID, id uuid.UUID) (*OrganizationAgentProvider, error) {
	var provider OrganizationAgentProvider

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

func FindAgentProviderBySlug(orgID uuid.UUID, slug string) (*OrganizationAgentProvider, error) {
	var provider OrganizationAgentProvider

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

// FindActiveAgentProviderByOrganization returns the organization's enabled agent
// provider — the one its agent sessions use — or (nil, nil) when none is
// configured (the caller then falls back to the installation-wide provider). If
// more than one is enabled, the most recently updated wins.
func FindActiveAgentProviderByOrganization(orgID uuid.UUID) (*OrganizationAgentProvider, error) {
	var provider OrganizationAgentProvider

	err := database.Conn().
		Where("organization_id = ?", orgID).
		Where("enabled = ?", true).
		Order("updated_at desc").
		First(&provider).
		Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &provider, nil
}
