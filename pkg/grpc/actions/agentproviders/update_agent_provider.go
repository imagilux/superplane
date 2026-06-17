package agentproviders

import (
	"context"

	"github.com/superplanehq/superplane/pkg/authentication"
	"github.com/superplanehq/superplane/pkg/crypto"
	pb "github.com/superplanehq/superplane/pkg/protos/agent_providers"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// UpdateAgentProvider replaces the editable fields with the request's desired
// state. slug and type are immutable. The API key is write-only: an empty value
// keeps the existing key.
func UpdateAgentProvider(ctx context.Context, req *pb.UpdateAgentProviderRequest, encryptor crypto.Encryptor) (*pb.UpdateAgentProviderResponse, error) {
	_, userIsSet := authentication.GetUserIdFromMetadata(ctx)
	if !userIsSet {
		return nil, status.Error(codes.Unauthenticated, "user not authenticated")
	}

	orgID, orgIsSet := authentication.GetOrganizationIdFromMetadata(ctx)
	if !orgIsSet {
		return nil, status.Error(codes.Unauthenticated, "user not authenticated")
	}

	provider, err := findProviderForRequest(orgID, req.Id)
	if err != nil {
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

	provider.DisplayName = req.DisplayName
	provider.BaseURL = req.BaseUrl
	provider.Model = req.Model
	provider.Enabled = req.Enabled

	if req.ApiKey != "" {
		if err := provider.SetAPIKey(ctx, encryptor, req.ApiKey); err != nil {
			return nil, status.Error(codes.Internal, "failed to encrypt API key")
		}
	}

	if err := provider.Save(); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to update agent provider: %v", err)
	}

	return &pb.UpdateAgentProviderResponse{Provider: serializeAgentProvider(provider)}, nil
}
