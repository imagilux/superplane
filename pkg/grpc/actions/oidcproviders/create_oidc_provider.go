package oidcproviders

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/superplanehq/superplane/pkg/authentication"
	"github.com/superplanehq/superplane/pkg/crypto"
	"github.com/superplanehq/superplane/pkg/models"
	pb "github.com/superplanehq/superplane/pkg/protos/oidc_providers"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func CreateOIDCProvider(ctx context.Context, req *pb.CreateOIDCProviderRequest, encryptor crypto.Encryptor) (*pb.CreateOIDCProviderResponse, error) {
	userID, userIsSet := authentication.GetUserIdFromMetadata(ctx)
	if !userIsSet {
		return nil, status.Error(codes.Unauthenticated, "user not authenticated")
	}

	orgID, orgIsSet := authentication.GetOrganizationIdFromMetadata(ctx)
	if !orgIsSet {
		return nil, status.Error(codes.Unauthenticated, "user not authenticated")
	}

	providerType, err := resolveProviderType(req.Type)
	if err != nil {
		return nil, err
	}

	if err := validateSlug(req.Slug); err != nil {
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
	if req.ClientSecret == "" {
		return nil, status.Error(codes.InvalidArgument, "client_secret is required")
	}

	orgUUID, err := uuid.Parse(orgID)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid organization ID")
	}

	createdByUUID, err := uuid.Parse(userID)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid user ID")
	}

	provider := models.NewOIDCProvider(orgUUID, &createdByUUID, req.Slug, req.DisplayName, providerType, req.IssuerUrl, req.ClientId, req.Scopes, req.AllowedEmailDomains, req.Enabled)
	if err := provider.SetClientSecret(ctx, encryptor, req.ClientSecret); err != nil {
		return nil, status.Error(codes.Internal, "failed to encrypt client secret")
	}

	if err := provider.Create(); err != nil {
		if errors.Is(err, models.ErrNameAlreadyUsed) {
			return nil, status.Error(codes.AlreadyExists, "an OIDC provider with this slug already exists in the organization")
		}
		return nil, status.Errorf(codes.Internal, "failed to create OIDC provider: %v", err)
	}

	return &pb.CreateOIDCProviderResponse{Provider: serializeOIDCProvider(provider)}, nil
}
