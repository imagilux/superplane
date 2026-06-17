package agentproviders

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/superplanehq/superplane/pkg/authentication"
	"github.com/superplanehq/superplane/pkg/crypto"
	"github.com/superplanehq/superplane/pkg/models"
	pb "github.com/superplanehq/superplane/pkg/protos/agent_providers"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func CreateAgentProvider(ctx context.Context, req *pb.CreateAgentProviderRequest, encryptor crypto.Encryptor) (*pb.CreateAgentProviderResponse, error) {
	userID, userIsSet := authentication.GetUserIdFromMetadata(ctx)
	if !userIsSet {
		return nil, status.Error(codes.Unauthenticated, "user not authenticated")
	}

	orgID, orgIsSet := authentication.GetOrganizationIdFromMetadata(ctx)
	if !orgIsSet {
		return nil, status.Error(codes.Unauthenticated, "user not authenticated")
	}

	providerType, err := resolveAgentProviderType(req.Type)
	if err != nil {
		return nil, err
	}

	if err := validateSlug(req.Slug); err != nil {
		return nil, err
	}

	if req.DisplayName == "" {
		return nil, status.Error(codes.InvalidArgument, "display_name is required")
	}
	if req.BaseUrl == "" {
		return nil, status.Error(codes.InvalidArgument, "base_url is required")
	}
	if req.Model == "" {
		return nil, status.Error(codes.InvalidArgument, "model is required")
	}

	orgUUID, err := uuid.Parse(orgID)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid organization ID")
	}

	createdByUUID, err := uuid.Parse(userID)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid user ID")
	}

	provider := models.NewAgentProvider(orgUUID, &createdByUUID, req.Slug, req.DisplayName, providerType, req.BaseUrl, req.Model, req.Enabled)

	// The API key is optional — unauthenticated local endpoints need none.
	if err := provider.SetAPIKey(ctx, encryptor, req.ApiKey); err != nil {
		return nil, status.Error(codes.Internal, "failed to encrypt API key")
	}

	if err := provider.Create(); err != nil {
		if errors.Is(err, models.ErrNameAlreadyUsed) {
			return nil, status.Error(codes.AlreadyExists, "an agent provider with this slug already exists in the organization")
		}
		return nil, status.Errorf(codes.Internal, "failed to create agent provider: %v", err)
	}

	return &pb.CreateAgentProviderResponse{Provider: serializeAgentProvider(provider)}, nil
}
