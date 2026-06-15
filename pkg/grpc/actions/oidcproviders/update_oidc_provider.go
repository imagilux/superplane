package oidcproviders

import (
	"context"

	"github.com/superplanehq/superplane/pkg/authentication"
	"github.com/superplanehq/superplane/pkg/crypto"
	pb "github.com/superplanehq/superplane/pkg/protos/oidc_providers"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// UpdateOIDCProvider replaces the editable fields with the request's desired state.
// slug and type are immutable. The client secret is write-only: an empty value
// keeps the existing secret.
func UpdateOIDCProvider(ctx context.Context, req *pb.UpdateOIDCProviderRequest, encryptor crypto.Encryptor) (*pb.UpdateOIDCProviderResponse, error) {
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
	if req.IssuerUrl == "" {
		return nil, status.Error(codes.InvalidArgument, "issuer_url is required")
	}
	if req.ClientId == "" {
		return nil, status.Error(codes.InvalidArgument, "client_id is required")
	}

	provider.DisplayName = req.DisplayName
	provider.IssuerURL = req.IssuerUrl
	provider.ClientID = req.ClientId
	provider.SetScopes(req.Scopes)
	provider.SetAllowedEmailDomains(req.AllowedEmailDomains)
	provider.Enabled = req.Enabled

	if req.ClientSecret != "" {
		if err := provider.SetClientSecret(ctx, encryptor, req.ClientSecret); err != nil {
			return nil, status.Error(codes.Internal, "failed to encrypt client secret")
		}
	}

	if err := provider.Save(); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to update OIDC provider: %v", err)
	}

	return &pb.UpdateOIDCProviderResponse{Provider: serializeOIDCProvider(provider)}, nil
}
