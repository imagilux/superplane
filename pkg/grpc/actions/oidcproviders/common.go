package oidcproviders

import (
	"regexp"

	"github.com/google/uuid"
	"github.com/superplanehq/superplane/pkg/models"
	pb "github.com/superplanehq/superplane/pkg/protos/oidc_providers"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// slugPattern restricts provider slugs to URL-safe values, since the slug appears
// in the SSO login path /auth/sso/{orgId}/{slug}.
var slugPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// serializeOIDCProvider maps the model to its API representation. The client secret
// is never included; HasClientSecret reports whether one is configured.
func serializeOIDCProvider(p *models.OrganizationOIDCProvider) *pb.OIDCProvider {
	out := &pb.OIDCProvider{
		Id:                  p.ID.String(),
		Slug:                p.Slug,
		DisplayName:         p.DisplayName,
		Type:                p.Type,
		IssuerUrl:           p.IssuerURL,
		ClientId:            p.ClientID,
		HasClientSecret:     p.HasClientSecret(),
		Scopes:              []string(p.Scopes),
		AllowedEmailDomains: []string(p.AllowedEmailDomains),
		Enabled:             p.Enabled,
		OrganizationId:      p.OrganizationID.String(),
	}

	if p.CreatedBy != nil {
		out.CreatedBy = p.CreatedBy.String()
	}
	if p.CreatedAt != nil {
		out.CreatedAt = timestamppb.New(*p.CreatedAt)
	}
	if p.UpdatedAt != nil {
		out.UpdatedAt = timestamppb.New(*p.UpdatedAt)
	}

	return out
}

// resolveProviderType validates the requested type. SAML is reserved but not yet
// implemented; anything else is rejected.
func resolveProviderType(t string) (string, error) {
	switch t {
	case "", models.OIDCProviderTypeOIDC:
		return models.OIDCProviderTypeOIDC, nil
	case models.OIDCProviderTypeSAML:
		return "", status.Error(codes.Unimplemented, "SAML providers are not yet supported")
	default:
		return "", status.Errorf(codes.InvalidArgument, "invalid provider type %q", t)
	}
}

func validateSlug(slug string) error {
	if slug == "" {
		return status.Error(codes.InvalidArgument, "slug is required")
	}
	if len(slug) > 64 || !slugPattern.MatchString(slug) {
		return status.Error(codes.InvalidArgument, "slug must be 1-64 characters of lowercase letters, digits, and hyphens")
	}
	return nil
}

// findProviderForRequest loads an organization-scoped provider by id, returning the
// appropriate gRPC status errors for invalid ids or a missing provider.
func findProviderForRequest(orgID, id string) (*models.OrganizationOIDCProvider, error) {
	if id == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}

	orgUUID, err := uuid.Parse(orgID)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid organization ID")
	}

	idUUID, err := uuid.Parse(id)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid provider ID")
	}

	provider, err := models.FindOIDCProviderByID(orgUUID, idUUID)
	if err != nil {
		return nil, status.Error(codes.NotFound, "OIDC provider not found")
	}

	return provider, nil
}
