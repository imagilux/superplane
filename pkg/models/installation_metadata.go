package models

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/superplanehq/superplane/pkg/crypto"
	"github.com/superplanehq/superplane/pkg/database"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const installationAgentAPIKeyAAD = "installation-agent-api-key"

const installationMetadataID = 1

type InstallationMetadata struct {
	ID                        int    `gorm:"primary_key"`
	InstallationID            string `gorm:"type:varchar(64)"`
	AllowPrivateNetworkAccess bool
	PasswordLoginDisabled     bool
	SSOLoginHintEnabled       bool
	SSOPromptNoneEnabled      bool
	SSOAutoLoginEnabled       bool
	// Pin the column: GORM's namer would otherwise map the mixed-case "IdP" to
	// sso_id_p_logout_enabled, which does not match the migration.
	SSOIdPLogoutEnabled bool `gorm:"column:sso_idp_logout_enabled"`
	// Installation-wide agent (LLM) provider, admin-configured. AgentProvider is
	// "" (use the environment-configured provider), "anthropic" (managed, its
	// secrets stay in the environment), or "openai" (the OpenAI-compatible
	// endpoint configured here). The API key is stored encrypted at rest (base64
	// of AES-GCM ciphertext). Columns pinned to avoid GORM initialism surprises.
	AgentProvider  string `gorm:"column:agent_provider"`
	AgentBaseURL   string `gorm:"column:agent_base_url"`
	AgentModel     string `gorm:"column:agent_model"`
	AgentAPIKeyEnc string `gorm:"column:agent_api_key_enc"`
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func GetInstallationMetadata() (*InstallationMetadata, error) {
	return GetInstallationMetadataInTransaction(database.Conn())
}

func GetInstallationID() (string, error) {
	return GetInstallationIDInTransaction(database.Conn())
}

func GetInstallationMetadataInTransaction(tx *gorm.DB) (*InstallationMetadata, error) {
	return findOrCreateInstallationMetadataInTransaction(tx)
}

func GetInstallationIDInTransaction(tx *gorm.DB) (string, error) {
	metadata, err := findOrCreateInstallationMetadataInTransaction(tx)
	if err != nil {
		return "", err
	}

	return metadata.InstallationID, nil
}

func UpdateInstallationMetadata(metadata *InstallationMetadata) error {
	return UpdateInstallationMetadataInTransaction(database.Conn(), metadata)
}

func UpdateInstallationMetadataInTransaction(tx *gorm.DB, metadata *InstallationMetadata) error {
	if _, err := findOrCreateInstallationMetadataInTransaction(tx); err != nil {
		return err
	}

	return tx.Model(&InstallationMetadata{}).
		Where("id = ?", installationMetadataID).
		Updates(map[string]any{
			"allow_private_network_access": metadata.AllowPrivateNetworkAccess,
			"password_login_disabled":      metadata.PasswordLoginDisabled,
			"sso_login_hint_enabled":       metadata.SSOLoginHintEnabled,
			"sso_prompt_none_enabled":      metadata.SSOPromptNoneEnabled,
			"sso_auto_login_enabled":       metadata.SSOAutoLoginEnabled,
			"sso_idp_logout_enabled":       metadata.SSOIdPLogoutEnabled,
			"agent_provider":               metadata.AgentProvider,
			"agent_base_url":               metadata.AgentBaseURL,
			"agent_model":                  metadata.AgentModel,
			"agent_api_key_enc":            metadata.AgentAPIKeyEnc,
			"updated_at":                   metadata.UpdatedAt,
		}).
		Error
}

func findOrCreateInstallationMetadataInTransaction(tx *gorm.DB) (*InstallationMetadata, error) {
	var metadata InstallationMetadata
	err := tx.Where("id = ?", installationMetadataID).First(&metadata).Error
	if err == nil {
		return &metadata, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	metadata = InstallationMetadata{
		ID:             installationMetadataID,
		InstallationID: uuid.NewString(),
	}

	if err := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoNothing: true,
	}).Create(&metadata).Error; err != nil {
		return nil, err
	}

	if err := tx.Where("id = ?", installationMetadataID).First(&metadata).Error; err != nil {
		return nil, err
	}

	return &metadata, nil
}

// UsesOpenAIAgent reports whether the installation is configured to use an
// admin-set OpenAI-compatible agent endpoint (provider "openai" with a base URL
// and model). When false, the agent service uses the environment-configured
// provider instead.
func (m *InstallationMetadata) UsesOpenAIAgent() bool {
	return m.AgentProvider == AgentProviderTypeOpenAI &&
		strings.TrimSpace(m.AgentBaseURL) != "" &&
		strings.TrimSpace(m.AgentModel) != ""
}

// HasAgentAPIKey reports whether an installation agent API key is stored, without exposing it.
func (m *InstallationMetadata) HasAgentAPIKey() bool {
	return m.AgentAPIKeyEnc != ""
}

// SetAgentAPIKey encrypts and stores the installation agent API key. An empty
// key clears it (unauthenticated local endpoints need none).
func (m *InstallationMetadata) SetAgentAPIKey(ctx context.Context, enc crypto.Encryptor, plaintext string) error {
	if plaintext == "" {
		m.AgentAPIKeyEnc = ""
		return nil
	}

	ciphertext, err := enc.Encrypt(ctx, []byte(plaintext), []byte(installationAgentAPIKeyAAD))
	if err != nil {
		return err
	}

	m.AgentAPIKeyEnc = base64.StdEncoding.EncodeToString(ciphertext)
	return nil
}

// DecryptAgentAPIKey returns the plaintext installation agent API key, or "" when none is stored.
func (m *InstallationMetadata) DecryptAgentAPIKey(ctx context.Context, enc crypto.Encryptor) (string, error) {
	if m.AgentAPIKeyEnc == "" {
		return "", nil
	}

	raw, err := base64.StdEncoding.DecodeString(m.AgentAPIKeyEnc)
	if err != nil {
		return "", err
	}

	plaintext, err := enc.Decrypt(ctx, raw, []byte(installationAgentAPIKeyAAD))
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}
