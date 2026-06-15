package oidcproviders

import (
	"context"
	"net/http"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/superplanehq/superplane/pkg/authentication"
	pb "github.com/superplanehq/superplane/pkg/protos/oidc_providers"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// discoveryHTTPClient bounds the server-side discovery fetch.
var discoveryHTTPClient = &http.Client{Timeout: 10 * time.Second}

// DiscoverOIDCProvider fetches the issuer's /.well-known/openid-configuration to
// validate it and report the supported scopes and claims, so the provider form
// can auto-resolve instead of requiring everything by hand.
//
// This makes a server-side request to an admin-supplied URL. It is gated to
// admins (oidc_providers:create) at the interceptor, which is the SSRF control:
// we intentionally do NOT block private IP ranges, because legitimate identity
// providers (e.g. a self-hosted Authelia/Keycloak) are commonly internal.
func DiscoverOIDCProvider(ctx context.Context, req *pb.DiscoverOIDCProviderRequest) (*pb.DiscoverOIDCProviderResponse, error) {
	if _, userIsSet := authentication.GetUserIdFromMetadata(ctx); !userIsSet {
		return nil, status.Error(codes.Unauthenticated, "user not authenticated")
	}
	if _, orgIsSet := authentication.GetOrganizationIdFromMetadata(ctx); !orgIsSet {
		return nil, status.Error(codes.Unauthenticated, "user not authenticated")
	}

	if req.IssuerUrl == "" {
		return nil, status.Error(codes.InvalidArgument, "issuer_url is required")
	}

	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()

	// A failed discovery (unreachable host, not an OIDC issuer) is a normal,
	// renderable outcome — return valid=false rather than a gRPC error.
	provider, err := oidc.NewProvider(oidc.ClientContext(ctx, discoveryHTTPClient), req.IssuerUrl)
	if err != nil {
		return &pb.DiscoverOIDCProviderResponse{Valid: false, Error: err.Error()}, nil
	}

	var meta struct {
		Issuer          string   `json:"issuer"`
		ScopesSupported []string `json:"scopes_supported"`
		ClaimsSupported []string `json:"claims_supported"`
	}
	if err := provider.Claims(&meta); err != nil {
		return &pb.DiscoverOIDCProviderResponse{Valid: false, Error: "could not parse the discovery document"}, nil
	}

	emailVerified := false
	for _, c := range meta.ClaimsSupported {
		if c == "email_verified" {
			emailVerified = true
			break
		}
	}

	return &pb.DiscoverOIDCProviderResponse{
		Valid:                  true,
		Issuer:                 meta.Issuer,
		ScopesSupported:        meta.ScopesSupported,
		ClaimsSupported:        meta.ClaimsSupported,
		EmailVerifiedSupported: emailVerified,
	}, nil
}
