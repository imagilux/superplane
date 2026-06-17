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
	"gorm.io/gorm"
)

// ArchiveAgentChat freezes the current chat under a generated title and returns
// a fresh, empty active chat for the canvas. The archived transcript remains
// browsable read-only.
func ArchiveAgentChat(ctx context.Context, svc AgentsService, orgID, userID string, req *pb.ArchiveAgentChatRequest) (*pb.ArchiveAgentChatResponse, error) {
	org, user, err := parseOrgUser(orgID, userID)
	if err != nil {
		return nil, err
	}
	chatID, err := uuid.Parse(req.ChatId)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid chat id")
	}

	fresh, err := svc.ArchiveCurrentSession(ctx, org, user, chatID)
	if err != nil {
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			return nil, status.Error(codes.NotFound, "agent chat not found")
		case errors.Is(err, agents.ErrSessionForbidden):
			return nil, status.Error(codes.PermissionDenied, "agent chat is not allowed")
		case errors.Is(err, agents.ErrSessionBusy):
			return nil, status.Error(codes.FailedPrecondition, "agent is still running; stop it before archiving")
		}
		log.WithError(err).WithField("chat_id", chatID).Error("failed to archive agent chat")
		return nil, status.Error(codes.Internal, "failed to archive agent chat")
	}
	return &pb.ArchiveAgentChatResponse{Chat: serializeChat(fresh)}, nil
}
