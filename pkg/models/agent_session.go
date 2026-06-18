package models

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/superplanehq/superplane/pkg/database"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	AgentSessionStatusIdle       = "idle"
	AgentSessionStatusStreaming  = "streaming"
	AgentSessionStatusFailed     = "failed"
	AgentSessionStatusTerminated = "terminated"
)

type AgentSession struct {
	ID                uuid.UUID `gorm:"primaryKey;default:uuid_generate_v4()"`
	OrganizationID    uuid.UUID
	UserID            uuid.UUID
	CanvasID          uuid.UUID
	Provider          string
	ProviderSessionID string
	Status            string
	Title             string
	LastActiveAt      *time.Time
	HeartbeatAt       *time.Time
	ArchivedAt        *time.Time
	CreatedAt         *time.Time
	UpdatedAt         *time.Time
}

func (AgentSession) TableName() string { return "agent_sessions" }

var ErrAgentSessionNotFound = errors.New("agent session not found")

func CreateAgentSessionInTransaction(tx *gorm.DB, session *AgentSession) error {
	return tx.Create(session).Error
}

func FindAgentSessionInTransaction(tx *gorm.DB, sessionID uuid.UUID) (*AgentSession, error) {
	var session AgentSession
	err := tx.Where("id = ?", sessionID).First(&session).Error
	if err != nil {
		return nil, err
	}
	return &session, nil
}

func FindAgentSession(sessionID uuid.UUID) (*AgentSession, error) {
	return FindAgentSessionInTransaction(database.Conn(), sessionID)
}

// FindAgentSessionForUserInTransaction enforces ownership: sessions are
// private per user, so a session is invisible to anyone but its creator.
func FindAgentSessionForUserInTransaction(tx *gorm.DB, organizationID, userID, sessionID uuid.UUID) (*AgentSession, error) {
	var session AgentSession
	err := tx.
		Where("id = ?", sessionID).
		Where("organization_id = ?", organizationID).
		Where("user_id = ?", userID).
		First(&session).
		Error
	if err != nil {
		return nil, err
	}
	return &session, nil
}

func FindAgentSessionForUser(organizationID, userID, sessionID uuid.UUID) (*AgentSession, error) {
	return FindAgentSessionForUserInTransaction(database.Conn(), organizationID, userID, sessionID)
}

// FindActiveAgentSessionByCanvasInTransaction returns the canvas's single
// non-archived session for the user. Archived sessions (archived_at IS NOT NULL)
// are excluded — they are browsable history, not the live chat. The partial
// unique index agent_sessions_active_user_canvas_idx guarantees at most one.
func FindActiveAgentSessionByCanvasInTransaction(tx *gorm.DB, organizationID, userID, canvasID uuid.UUID) (*AgentSession, error) {
	var session AgentSession
	err := tx.
		Where("organization_id = ?", organizationID).
		Where("user_id = ?", userID).
		Where("canvas_id = ?", canvasID).
		Where("archived_at IS NULL").
		First(&session).
		Error
	if err != nil {
		return nil, err
	}
	return &session, nil
}

// ArchiveAgentSessionInTransaction freezes the active session under a title and
// stamps archived_at. The archived_at IS NULL guard makes a double-archive a
// no-op (RowsAffected == 0), so the caller can detect an already-archived row.
func ArchiveAgentSessionInTransaction(tx *gorm.DB, sessionID uuid.UUID, title string) (int64, error) {
	now := time.Now()
	result := tx.Model(&AgentSession{}).
		Where("id = ?", sessionID).
		Where("archived_at IS NULL").
		Updates(map[string]any{
			"title":       title,
			"archived_at": &now,
			"updated_at":  &now,
		})
	return result.RowsAffected, result.Error
}

// ListArchivedAgentSessionsByCanvas returns a page of the user's archived
// sessions for a canvas (newest first) plus the total archived count, backing
// the archived-sessions drawer's pagination.
func ListArchivedAgentSessionsByCanvas(organizationID, userID, canvasID uuid.UUID, offset, limit int) ([]AgentSession, int64, error) {
	archived := func() *gorm.DB {
		return database.Conn().Model(&AgentSession{}).
			Where("organization_id = ?", organizationID).
			Where("user_id = ?", userID).
			Where("canvas_id = ?", canvasID).
			Where("archived_at IS NOT NULL")
	}

	var total int64
	if err := archived().Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var sessions []AgentSession
	if err := archived().
		Order("created_at DESC, id DESC").
		Offset(offset).
		Limit(limit).
		Find(&sessions).Error; err != nil {
		return nil, 0, err
	}
	return sessions, total, nil
}

func ListAgentSessionsForCanvasInTransaction(tx *gorm.DB, organizationID, canvasID uuid.UUID) ([]AgentSession, error) {
	var sessions []AgentSession
	err := tx.
		Where("organization_id = ?", organizationID).
		Where("canvas_id = ?", canvasID).
		Find(&sessions).
		Error
	return sessions, err
}

func ListAgentSessionsForOrganizationInTransaction(tx *gorm.DB, organizationID uuid.UUID) ([]AgentSession, error) {
	var sessions []AgentSession
	err := tx.
		Where("organization_id = ?", organizationID).
		Find(&sessions).
		Error
	return sessions, err
}

func UpdateAgentSessionStatusInTransaction(tx *gorm.DB, sessionID uuid.UUID, status string) error {
	now := time.Now()
	return tx.Model(&AgentSession{}).
		Where("id = ?", sessionID).
		Updates(map[string]any{
			"status":         status,
			"last_active_at": &now,
			"updated_at":     &now,
			// Clear so a new streaming turn starts in the legacy-cutoff
			// branch until the worker writes its first heartbeat.
			"heartbeat_at": gorm.Expr("NULL"),
		}).
		Error
}

func UpdateAgentSessionStatus(sessionID uuid.UUID, status string) error {
	return UpdateAgentSessionStatusInTransaction(database.Conn(), sessionID, status)
}

func UpdateAgentSessionStatusIfUnchanged(sessionID uuid.UUID, status string, unchangedSince *time.Time) (bool, error) {
	return UpdateAgentSessionStatusIfUnchangedInTransaction(database.Conn(), sessionID, status, unchangedSince)
}

func IsAgentSessionStreaming(sessionID uuid.UUID) (bool, error) {
	var count int64
	if err := database.Conn().Model(&AgentSession{}).
		Where("id = ?", sessionID).
		Where("status = ?", AgentSessionStatusStreaming).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// TouchAgentSessionHeartbeat uses UpdateColumn so updated_at stays put —
// it's the optimistic-concurrency key for the per-turn idle transition.
func TouchAgentSessionHeartbeat(sessionID uuid.UUID) error {
	now := time.Now()
	return database.Conn().Model(&AgentSession{}).
		Where("id = ?", sessionID).
		Where("status = ?", AgentSessionStatusStreaming).
		UpdateColumn("heartbeat_at", &now).
		Error
}

func UpdateAgentSessionProviderSessionInTransaction(tx *gorm.DB, sessionID uuid.UUID, providerSessionID, status string) error {
	now := time.Now()
	return tx.Model(&AgentSession{}).
		Where("id = ?", sessionID).
		Updates(map[string]any{
			"provider_session_id": providerSessionID,
			"status":              status,
			"last_active_at":      &now,
			"updated_at":          &now,
			"heartbeat_at":        gorm.Expr("NULL"),
		}).
		Error
}

func UpdateAgentSessionStatusIfUnchangedInTransaction(tx *gorm.DB, sessionID uuid.UUID, status string, unchangedSince *time.Time) (bool, error) {
	now := time.Now()
	query := tx.Model(&AgentSession{}).Where("id = ?", sessionID)
	if unchangedSince == nil {
		query = query.Where("updated_at IS NULL")
	} else {
		query = query.Where("updated_at = ?", *unchangedSince)
	}

	result := query.Updates(map[string]any{
		"status":         status,
		"last_active_at": &now,
		"updated_at":     &now,
		"heartbeat_at":   gorm.Expr("NULL"),
	})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func DeleteAgentSessionsForCanvasInTransaction(tx *gorm.DB, organizationID, canvasID uuid.UUID) error {
	return tx.
		Where("organization_id = ?", organizationID).
		Where("canvas_id = ?", canvasID).
		Delete(&AgentSession{}).
		Error
}

func DeleteAgentSessionsForOrganizationInTransaction(tx *gorm.DB, organizationID uuid.UUID) error {
	return tx.
		Where("organization_id = ?", organizationID).
		Delete(&AgentSession{}).
		Error
}

func LockAgentSessionInTransaction(tx *gorm.DB, sessionID uuid.UUID) (*AgentSession, error) {
	var session AgentSession
	err := tx.
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ?", sessionID).
		First(&session).
		Error
	if err != nil {
		return nil, err
	}
	return &session, nil
}

// FailStuckStreamingSessions flags leaked streaming rows. Heartbeated rows
// use heartbeatCutoff (tight); rows with no heartbeat yet — pre-heartbeat
// binaries, or new turns before the worker's first tick — fall back to
// updated_at with legacyCutoff (loose, sized above agentStreamTimeout).
func FailStuckStreamingSessions(heartbeatCutoff, legacyCutoff time.Time) ([]AgentSession, error) {
	var stuck []AgentSession
	err := database.Conn().Transaction(func(tx *gorm.DB) error {
		if err := tx.
			Where("status = ?", AgentSessionStatusStreaming).
			Where("(heartbeat_at IS NOT NULL AND heartbeat_at < ?) OR (heartbeat_at IS NULL AND updated_at < ?)", heartbeatCutoff, legacyCutoff).
			Find(&stuck).Error; err != nil {
			return err
		}
		if len(stuck) == 0 {
			return nil
		}
		ids := make([]uuid.UUID, 0, len(stuck))
		for _, s := range stuck {
			ids = append(ids, s.ID)
		}
		now := time.Now()
		return tx.Model(&AgentSession{}).
			Where("id IN ?", ids).
			Updates(map[string]any{
				"status":       AgentSessionStatusFailed,
				"updated_at":   &now,
				"heartbeat_at": gorm.Expr("NULL"),
			}).Error
	})
	if err != nil {
		return nil, err
	}
	return stuck, nil
}
