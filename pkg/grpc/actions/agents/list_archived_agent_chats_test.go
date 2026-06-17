package agents_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	actionsagents "github.com/superplanehq/superplane/pkg/grpc/actions/agents"
	"github.com/superplanehq/superplane/pkg/models"
	pb "github.com/superplanehq/superplane/pkg/protos/agents"
	"github.com/superplanehq/superplane/test/support"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestListArchivedAgentChats_Paginates(t *testing.T) {
	r := support.Setup(t)
	defer r.Close()
	canvas := setupCanvas(t, r)

	var gotOffset, gotLimit int
	svc := &stubService{
		listArchived: func(_ context.Context, o, u, c uuid.UUID, offset, limit int) ([]models.AgentSession, int64, error) {
			assert.Equal(t, r.Organization.ID, o)
			assert.Equal(t, r.User, u)
			assert.Equal(t, canvas.ID, c)
			gotOffset, gotLimit = offset, limit
			return []models.AgentSession{
				{ID: uuid.New(), CanvasID: canvas.ID, Title: "newest", ArchivedAt: now(), CreatedAt: now()},
				{ID: uuid.New(), CanvasID: canvas.ID, Title: "older", ArchivedAt: now(), CreatedAt: now()},
			}, 5, nil
		},
	}

	resp, err := actionsagents.ListArchivedAgentChats(context.Background(), svc, r.Organization.ID.String(), r.User.String(), &pb.ListArchivedAgentChatsRequest{
		CanvasId: canvas.ID.String(),
		Page:     2,
		PageSize: 2,
	})
	require.NoError(t, err)
	assert.Equal(t, 2, gotOffset, "offset = (page-1)*pageSize")
	assert.Equal(t, 2, gotLimit)
	assert.Equal(t, int32(5), resp.Total)
	assert.Equal(t, int32(2), resp.Page)
	assert.Equal(t, int32(2), resp.PageSize)
	require.Len(t, resp.Chats, 2)
	assert.Equal(t, "newest", resp.Chats[0].Title)
	require.NotNil(t, resp.Chats[0].ArchivedAt)
}

func TestListArchivedAgentChats_DefaultsPagination(t *testing.T) {
	r := support.Setup(t)
	defer r.Close()
	canvas := setupCanvas(t, r)

	var gotOffset, gotLimit int
	svc := &stubService{
		listArchived: func(_ context.Context, _, _, _ uuid.UUID, offset, limit int) ([]models.AgentSession, int64, error) {
			gotOffset, gotLimit = offset, limit
			return nil, 0, nil
		},
	}

	resp, err := actionsagents.ListArchivedAgentChats(context.Background(), svc, r.Organization.ID.String(), r.User.String(), &pb.ListArchivedAgentChatsRequest{CanvasId: canvas.ID.String()})
	require.NoError(t, err)
	assert.Equal(t, 0, gotOffset)
	assert.Equal(t, 10, gotLimit, "default page size")
	assert.Equal(t, int32(1), resp.Page)
	assert.Equal(t, int32(10), resp.PageSize)
	assert.Empty(t, resp.Chats)
}

func TestListArchivedAgentChats_RejectsInvalidCanvas(t *testing.T) {
	r := support.Setup(t)
	defer r.Close()

	_, err := actionsagents.ListArchivedAgentChats(context.Background(), &stubService{}, r.Organization.ID.String(), r.User.String(), &pb.ListArchivedAgentChatsRequest{CanvasId: "nope"})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestListArchivedAgentChats_NotFoundCanvas(t *testing.T) {
	r := support.Setup(t)
	defer r.Close()

	_, err := actionsagents.ListArchivedAgentChats(context.Background(), &stubService{}, r.Organization.ID.String(), r.User.String(), &pb.ListArchivedAgentChatsRequest{CanvasId: uuid.NewString()})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}
