package oidcproviders

import (
	"context"

	"github.com/google/uuid"
	"github.com/superplanehq/superplane/pkg/authentication"
	"github.com/superplanehq/superplane/pkg/models"
	pb "github.com/superplanehq/superplane/pkg/protos/oidc_providers"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func ListOIDCProviders(ctx context.Context) (*pb.ListOIDCProvidersResponse, error) {
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

	providers, err := models.FindOIDCProvidersByOrganization(orgUUID)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to list OIDC providers")
	}

	out := make([]*pb.OIDCProvider, len(providers))
	for i := range providers {
		out[i] = serializeOIDCProvider(&providers[i])
	}

	return &pb.ListOIDCProvidersResponse{Providers: out}, nil
}
