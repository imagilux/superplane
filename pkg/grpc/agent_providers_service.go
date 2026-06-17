package grpc

import (
	"context"

	"github.com/superplanehq/superplane/pkg/crypto"
	"github.com/superplanehq/superplane/pkg/grpc/actions/agentproviders"
	pb "github.com/superplanehq/superplane/pkg/protos/agent_providers"
)

type AgentProvidersService struct {
	pb.UnimplementedAgentProvidersServer
	encryptor crypto.Encryptor
}

func NewAgentProvidersService(encryptor crypto.Encryptor) *AgentProvidersService {
	return &AgentProvidersService{encryptor: encryptor}
}

func (s *AgentProvidersService) CreateAgentProvider(ctx context.Context, req *pb.CreateAgentProviderRequest) (*pb.CreateAgentProviderResponse, error) {
	return agentproviders.CreateAgentProvider(ctx, req, s.encryptor)
}

func (s *AgentProvidersService) ListAgentProviders(ctx context.Context, req *pb.ListAgentProvidersRequest) (*pb.ListAgentProvidersResponse, error) {
	return agentproviders.ListAgentProviders(ctx)
}

func (s *AgentProvidersService) DescribeAgentProvider(ctx context.Context, req *pb.DescribeAgentProviderRequest) (*pb.DescribeAgentProviderResponse, error) {
	return agentproviders.DescribeAgentProvider(ctx, req)
}

func (s *AgentProvidersService) UpdateAgentProvider(ctx context.Context, req *pb.UpdateAgentProviderRequest) (*pb.UpdateAgentProviderResponse, error) {
	return agentproviders.UpdateAgentProvider(ctx, req, s.encryptor)
}

func (s *AgentProvidersService) DeleteAgentProvider(ctx context.Context, req *pb.DeleteAgentProviderRequest) (*pb.DeleteAgentProviderResponse, error) {
	return agentproviders.DeleteAgentProvider(ctx, req)
}
