package oidcproviders

import (
	"context"
	"net/url"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	log "github.com/sirupsen/logrus"
	"github.com/superplanehq/superplane/pkg/authentication"
	"github.com/superplanehq/superplane/pkg/authentication/sso"
	pb "github.com/superplanehq/superplane/pkg/protos/oidc_providers"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// discoveryHTTPClient bounds and SSRF-guards the server-side discovery fetch
// (see pkg/authentication/sso.NewGuardedHTTPClient).
var discoveryHTTPClient = sso.NewGuardedHTTPClient(10 * time.Second)

// DiscoverOIDCProvider fetches the issuer's /.well-known/openid-configuration to
// validate it and report the supported scopes and claims, so the provider form
// can auto-resolve instead of requiring everything by hand.
//
// It makes a server-side request to an admin-supplied URL. Defenses against
// SSRF: it is gated to admins (oidc_providers:create) at the interceptor; the
// issuer must be an http(s) URL; the HTTP client refuses to dial loopback /
// link-local (cloud metadata) / unspecified / multicast addresses and is
// DNS-rebind safe; and the raw fetch error is logged server-side rather than
// returned. RFC1918 private ranges are intentionally NOT blocked, because
// legitimate self-hosted IdPs (e.g. Authelia/Keycloak) commonly live on private
// networks — the whole point of generic OIDC SSO.
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

	if u, err := url.Parse(req.IssuerUrl); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return &pb.DiscoverOIDCProviderResponse{Valid: false, Error: "issuer must be a valid http(s) URL"}, nil
	}

	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()

	// A failed discovery (unreachable host, blocked address, not an OIDC issuer)
	// is a normal, renderable outcome — return valid=false with a generic message
	// and log the detail rather than echoing it (which could leak topology).
	provider, err := oidc.NewProvider(oidc.ClientContext(ctx, discoveryHTTPClient), req.IssuerUrl)
	if err != nil {
		log.Warnf("OIDC discovery probe failed for issuer %q: %v", req.IssuerUrl, err)
		return &pb.DiscoverOIDCProviderResponse{
			Valid: false,
			Error: "could not reach or validate the issuer; check the URL and that it exposes /.well-known/openid-configuration",
		}, nil
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
