package agentproviders

import (
	"regexp"

	"github.com/google/uuid"
	"github.com/superplanehq/superplane/pkg/models"
	pb "github.com/superplanehq/superplane/pkg/protos/agent_providers"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// slugPattern restricts provider slugs to URL-safe values so they identify the
// provider unambiguously within an organization and stay safe in API paths.
var slugPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// serializeAgentProvider maps the model to its API representation. The API key is
// never included; HasAPIKey reports whether one is configured.
func serializeAgentProvider(p *models.OrganizationAgentProvider) *pb.AgentProvider {
	out := &pb.AgentProvider{
		Id:             p.ID.String(),
		Slug:           p.Slug,
		DisplayName:    p.DisplayName,
		Type:           p.Type,
		BaseUrl:        p.BaseURL,
		Model:          p.Model,
		HasApiKey:      p.HasAPIKey(),
		Enabled:        p.Enabled,
		OrganizationId: p.OrganizationID.String(),
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

// resolveAgentProviderType validates the requested type. Only the
// OpenAI-compatible kind is configurable per-organization today; the Anthropic
// managed-agent path stays installation-level. Anything else is rejected.
func resolveAgentProviderType(t string) (string, error) {
	switch t {
	case "", models.AgentProviderTypeOpenAI:
		return models.AgentProviderTypeOpenAI, nil
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

// findProviderForRequest loads an organization-scoped provider by id, returning
// the appropriate gRPC status errors for invalid ids or a missing provider.
func findProviderForRequest(orgID, id string) (*models.OrganizationAgentProvider, error) {
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

	provider, err := models.FindAgentProviderByID(orgUUID, idUUID)
	if err != nil {
		return nil, status.Error(codes.NotFound, "agent provider not found")
	}

	return provider, nil
}
