package oidcproviders

import (
	"context"

	"github.com/superplanehq/superplane/pkg/authentication"
	pb "github.com/superplanehq/superplane/pkg/protos/oidc_providers"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func DeleteOIDCProvider(ctx context.Context, req *pb.DeleteOIDCProviderRequest) (*pb.DeleteOIDCProviderResponse, error) {
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

	if err := provider.Delete(); err != nil {
		return nil, status.Error(codes.Internal, "failed to delete OIDC provider")
	}

	return &pb.DeleteOIDCProviderResponse{}, nil
}
