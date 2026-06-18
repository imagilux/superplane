package agents

import (
	"context"
	"errors"

	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
	"github.com/superplanehq/superplane/pkg/agents"
	pb "github.com/superplanehq/superplane/pkg/protos/agents"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	defaultArchivedPageSize = 10
	maxArchivedPageSize     = 100
)

// ListArchivedAgentChats returns the user's archived chats for a canvas, newest
// first, paginated (1-based page).
func ListArchivedAgentChats(ctx context.Context, svc AgentsService, orgID, userID string, req *pb.ListArchivedAgentChatsRequest) (*pb.ListArchivedAgentChatsResponse, error) {
	org, user, err := parseOrgUser(orgID, userID)
	if err != nil {
		return nil, err
	}
	canvas, err := uuid.Parse(req.CanvasId)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid canvas id")
	}
	if err := ensureCanvas(org, canvas); err != nil {
		return nil, err
	}

	page := int(req.Page)
	if page <= 0 {
		page = 1
	}
	pageSize := int(req.PageSize)
	if pageSize <= 0 {
		pageSize = defaultArchivedPageSize
	}
	if pageSize > maxArchivedPageSize {
		pageSize = maxArchivedPageSize
	}
	offset := (page - 1) * pageSize

	sessions, total, err := svc.ListArchivedSessions(ctx, org, user, canvas, offset, pageSize)
	if err != nil {
		if errors.Is(err, agents.ErrSessionForbidden) {
			return nil, status.Error(codes.PermissionDenied, "agent chat is not allowed")
		}
		log.WithError(err).WithField("canvas_id", canvas).Error("failed to list archived agent chats")
		return nil, status.Error(codes.Internal, "failed to list archived agent chats")
	}

	out := make([]*pb.AgentChatInfo, 0, len(sessions))
	for i := range sessions {
		out = append(out, serializeChat(&sessions[i]))
	}
	return &pb.ListArchivedAgentChatsResponse{
		Chats:    out,
		Total:    int32(total),
		Page:     int32(page),
		PageSize: int32(pageSize),
	}, nil
}
