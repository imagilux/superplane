package agents_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/superplanehq/superplane/pkg/agents"
	actionsagents "github.com/superplanehq/superplane/pkg/grpc/actions/agents"
	"github.com/superplanehq/superplane/pkg/models"
	pb "github.com/superplanehq/superplane/pkg/protos/agents"
	"github.com/superplanehq/superplane/test/support"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

func TestArchiveAgentChat_ReturnsFreshChat(t *testing.T) {
	r := support.Setup(t)
	defer r.Close()
	canvas := setupCanvas(t, r)
	oldID := uuid.New()
	freshID := uuid.New()

	svc := &stubService{
		archiveSession: func(_ context.Context, o, u, id uuid.UUID) (*models.AgentSession, error) {
			assert.Equal(t, r.Organization.ID, o)
			assert.Equal(t, r.User, u)
			assert.Equal(t, oldID, id)
			return &models.AgentSession{
				ID:        freshID,
				CanvasID:  canvas.ID,
				Provider:  "openai",
				Status:    models.AgentSessionStatusIdle,
				CreatedAt: now(),
				UpdatedAt: now(),
			}, nil
		},
	}

	resp, err := actionsagents.ArchiveAgentChat(context.Background(), svc, r.Organization.ID.String(), r.User.String(), &pb.ArchiveAgentChatRequest{ChatId: oldID.String()})
	require.NoError(t, err)
	require.NotNil(t, resp.Chat)
	assert.Equal(t, freshID.String(), resp.Chat.Id)
	assert.Empty(t, resp.Chat.Title, "fresh chat has no title")
}

func TestArchiveAgentChat_RejectsInvalidChat(t *testing.T) {
	r := support.Setup(t)
	defer r.Close()

	_, err := actionsagents.ArchiveAgentChat(context.Background(), &stubService{}, r.Organization.ID.String(), r.User.String(), &pb.ArchiveAgentChatRequest{ChatId: "nope"})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestArchiveAgentChat_BusyReturnsFailedPrecondition(t *testing.T) {
	r := support.Setup(t)
	defer r.Close()

	svc := &stubService{
		archiveSession: func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (*models.AgentSession, error) {
			return nil, agents.ErrSessionBusy
		},
	}
	_, err := actionsagents.ArchiveAgentChat(context.Background(), svc, r.Organization.ID.String(), r.User.String(), &pb.ArchiveAgentChatRequest{ChatId: uuid.NewString()})
	require.Error(t, err)
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
}

func TestArchiveAgentChat_NotFound(t *testing.T) {
	r := support.Setup(t)
	defer r.Close()

	svc := &stubService{
		archiveSession: func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (*models.AgentSession, error) {
			return nil, gorm.ErrRecordNotFound
		},
	}
	_, err := actionsagents.ArchiveAgentChat(context.Background(), svc, r.Organization.ID.String(), r.User.String(), &pb.ArchiveAgentChatRequest{ChatId: uuid.NewString()})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}
