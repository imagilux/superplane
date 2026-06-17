package agentproviders

import (
	"context"

	"github.com/google/uuid"
	"github.com/superplanehq/superplane/pkg/authentication"
	"github.com/superplanehq/superplane/pkg/models"
	pb "github.com/superplanehq/superplane/pkg/protos/agent_providers"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func ListAgentProviders(ctx context.Context) (*pb.ListAgentProvidersResponse, error) {
	_, userIsSet := authentication.GetUserIdFromMetadata(ctx)
	if !userIsSet {
		return nil, status.Error(codes.Unauthenticated, "user not authenticated")
	}

	orgID, orgIsSet := authentication.GetOrganizationIdFromMetadata(ctx)
	if !orgIsSet {
		return nil, status.Error(codes.Unauthenticated, "user not authenticated")
	}

	orgUUID, err := uuid.Parse(orgID)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid organization ID")
	}

	providers, err := models.FindAgentProvidersByOrganization(orgUUID)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to list agent providers")
	}

	out := make([]*pb.AgentProvider, len(providers))
	for i := range providers {
		out[i] = serializeAgentProvider(&providers[i])
	}

	return &pb.ListAgentProvidersResponse{Providers: out}, nil
}
