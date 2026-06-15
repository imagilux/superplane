package grpc

import (
	"context"

	"github.com/superplanehq/superplane/pkg/crypto"
	"github.com/superplanehq/superplane/pkg/grpc/actions/oidcproviders"
	pb "github.com/superplanehq/superplane/pkg/protos/oidc_providers"
)

type OIDCProvidersService struct {
	pb.UnimplementedOIDCProvidersServer
	encryptor crypto.Encryptor
}

func NewOIDCProvidersService(encryptor crypto.Encryptor) *OIDCProvidersService {
	return &OIDCProvidersService{encryptor: encryptor}
}

func (s *OIDCProvidersService) CreateOIDCProvider(ctx context.Context, req *pb.CreateOIDCProviderRequest) (*pb.CreateOIDCProviderResponse, error) {
	return oidcproviders.CreateOIDCProvider(ctx, req, s.encryptor)
}

func (s *OIDCProvidersService) ListOIDCProviders(ctx context.Context, req *pb.ListOIDCProvidersRequest) (*pb.ListOIDCProvidersResponse, error) {
	return oidcproviders.ListOIDCProviders(ctx)
}

func (s *OIDCProvidersService) DescribeOIDCProvider(ctx context.Context, req *pb.DescribeOIDCProviderRequest) (*pb.DescribeOIDCProviderResponse, error) {
	return oidcproviders.DescribeOIDCProvider(ctx, req)
}

func (s *OIDCProvidersService) UpdateOIDCProvider(ctx context.Context, req *pb.UpdateOIDCProviderRequest) (*pb.UpdateOIDCProviderResponse, error) {
	return oidcproviders.UpdateOIDCProvider(ctx, req, s.encryptor)
}

func (s *OIDCProvidersService) DeleteOIDCProvider(ctx context.Context, req *pb.DeleteOIDCProviderRequest) (*pb.DeleteOIDCProviderResponse, error) {
	return oidcproviders.DeleteOIDCProvider(ctx, req)
}
