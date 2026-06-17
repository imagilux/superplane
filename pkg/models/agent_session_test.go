package models

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/superplanehq/superplane/pkg/database"
	"gorm.io/gorm"
)

// createSessionAt inserts an agent session and pins its created_at to ts so the
// archived-list ordering is deterministic (it sorts by created_at DESC).
func createSessionAt(t *testing.T, org, user, canvas uuid.UUID, ts time.Time) *AgentSession {
	t.Helper()
	session := &AgentSession{
		ID:                uuid.New(),
		OrganizationID:    org,
		UserID:            user,
		CanvasID:          canvas,
		Provider:          "openai",
		ProviderSessionID: uuid.NewString(),
		Status:            AgentSessionStatusIdle,
	}
	require.NoError(t, CreateAgentSessionInTransaction(database.Conn(), session))
	require.NoError(t, database.Conn().Model(&AgentSession{}).
		Where("id = ?", session.ID).
		UpdateColumn("created_at", ts).Error)
	return session
}

func TestFindActiveAgentSessionByCanvas_ExcludesArchived(t *testing.T) {
	org, user, canvas := uuid.New(), uuid.New(), uuid.New()
	t.Cleanup(func() { _ = database.Conn().Where("canvas_id = ?", canvas).Delete(&AgentSession{}).Error })

	first := createSessionAt(t, org, user, canvas, time.Now())

	found, err := FindActiveAgentSessionByCanvasInTransaction(database.Conn(), org, user, canvas)
	require.NoError(t, err)
	assert.Equal(t, first.ID, found.ID)

	// Archiving the active session leaves no active session for the canvas.
	affected, err := ArchiveAgentSessionInTransaction(database.Conn(), first.ID, "first topic")
	require.NoError(t, err)
	assert.Equal(t, int64(1), affected)

	_, err = FindActiveAgentSessionByCanvasInTransaction(database.Conn(), org, user, canvas)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)

	// A fresh session is active again (the partial unique index permits it
	// alongside the archived row).
	second := createSessionAt(t, org, user, canvas, time.Now())
	found, err = FindActiveAgentSessionByCanvasInTransaction(database.Conn(), org, user, canvas)
	require.NoError(t, err)
	assert.Equal(t, second.ID, found.ID)
}

func TestArchiveAgentSessionInTransaction_StampsTitleAndIsIdempotent(t *testing.T) {
	org, user, canvas := uuid.New(), uuid.New(), uuid.New()
	t.Cleanup(func() { _ = database.Conn().Where("canvas_id = ?", canvas).Delete(&AgentSession{}).Error })

	session := createSessionAt(t, org, user, canvas, time.Now())

	affected, err := ArchiveAgentSessionInTransaction(database.Conn(), session.ID, "17/06/26 — JIRA URL fix")
	require.NoError(t, err)
	assert.Equal(t, int64(1), affected)

	var stored AgentSession
	require.NoError(t, database.Conn().Where("id = ?", session.ID).First(&stored).Error)
	assert.Equal(t, "17/06/26 — JIRA URL fix", stored.Title)
	require.NotNil(t, stored.ArchivedAt)

	// Re-archiving is a no-op: the archived_at IS NULL guard matches nothing.
	affected, err = ArchiveAgentSessionInTransaction(database.Conn(), session.ID, "overwritten?")
	require.NoError(t, err)
	assert.Equal(t, int64(0), affected)

	require.NoError(t, database.Conn().Where("id = ?", session.ID).First(&stored).Error)
	assert.Equal(t, "17/06/26 — JIRA URL fix", stored.Title)
}

func TestListArchivedAgentSessionsByCanvas_PaginatesNewestFirst(t *testing.T) {
	org, user, canvas := uuid.New(), uuid.New(), uuid.New()
	otherCanvas := uuid.New()
	t.Cleanup(func() {
		_ = database.Conn().Where("canvas_id IN ?", []uuid.UUID{canvas, otherCanvas}).Delete(&AgentSession{}).Error
	})

	base := time.Now().Add(-72 * time.Hour)
	// Three archived sessions for the target canvas, oldest → newest.
	for i, title := range []string{"oldest", "middle", "newest"} {
		s := createSessionAt(t, org, user, canvas, base.Add(time.Duration(i)*time.Hour))
		_, err := ArchiveAgentSessionInTransaction(database.Conn(), s.ID, title)
		require.NoError(t, err)
	}
	// An active session for the same canvas must be excluded.
	createSessionAt(t, org, user, canvas, time.Now())
	// An archived session for a different canvas must be excluded.
	other := createSessionAt(t, org, user, otherCanvas, time.Now())
	_, err := ArchiveAgentSessionInTransaction(database.Conn(), other.ID, "other canvas")
	require.NoError(t, err)

	page1, total, err := ListArchivedAgentSessionsByCanvas(org, user, canvas, 0, 2)
	require.NoError(t, err)
	assert.Equal(t, int64(3), total)
	require.Len(t, page1, 2)
	assert.Equal(t, "newest", page1[0].Title)
	assert.Equal(t, "middle", page1[1].Title)

	page2, total, err := ListArchivedAgentSessionsByCanvas(org, user, canvas, 2, 2)
	require.NoError(t, err)
	assert.Equal(t, int64(3), total)
	require.Len(t, page2, 1)
	assert.Equal(t, "oldest", page2[0].Title)
}
