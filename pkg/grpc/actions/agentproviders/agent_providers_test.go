package agentproviders

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	pb "github.com/superplanehq/superplane/pkg/protos/agent_providers"
	"github.com/superplanehq/superplane/test/support"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func authCtx(orgID, userID string) context.Context {
	return metadata.NewIncomingContext(
		context.Background(),
		metadata.Pairs("x-organization-id", orgID, "x-user-id", userID),
	)
}

func TestCreateAgentProvider(t *testing.T) {
	r := support.Setup(t)
	ctx := authCtx(r.Organization.ID.String(), r.User.String())

	t.Run("creates a provider and never returns the API key", func(t *testing.T) {
		resp, err := CreateAgentProvider(ctx, &pb.CreateAgentProviderRequest{
			Slug:        "openrouter",
			DisplayName: "OpenRouter",
			BaseUrl:     "https://openrouter.ai/api/v1",
			Model:       "anthropic/claude-3.5-sonnet",
			ApiKey:      "super-secret",
			Enabled:     true,
		}, r.Encryptor)
		require.NoError(t, err)
		assert.Equal(t, "openrouter", resp.Provider.Slug)
		assert.Equal(t, "openai", resp.Provider.Type)
		assert.Equal(t, "https://openrouter.ai/api/v1", resp.Provider.BaseUrl)
		assert.Equal(t, "anthropic/claude-3.5-sonnet", resp.Provider.Model)
		assert.True(t, resp.Provider.HasApiKey)
		assert.True(t, resp.Provider.Enabled)
		assert.Equal(t, r.Organization.ID.String(), resp.Provider.OrganizationId)
	})

	t.Run("creates a provider without an API key (unauthenticated local endpoint)", func(t *testing.T) {
		resp, err := CreateAgentProvider(ctx, &pb.CreateAgentProviderRequest{
			Slug:        "local-llama",
			DisplayName: "Local llama.cpp",
			BaseUrl:     "http://localhost:8080/v1",
			Model:       "qwen3",
			Enabled:     true,
		}, r.Encryptor)
		require.NoError(t, err)
		assert.False(t, resp.Provider.HasApiKey)
		assert.Equal(t, "openai", resp.Provider.Type)
	})

	t.Run("rejects an unsupported provider type", func(t *testing.T) {
		_, err := CreateAgentProvider(ctx, &pb.CreateAgentProviderRequest{
			Slug: "anthropic", DisplayName: "Anthropic", Type: "anthropic",
			BaseUrl: "https://api.anthropic.com", Model: "claude",
		}, r.Encryptor)
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("validates required fields and slug format (API key optional)", func(t *testing.T) {
		cases := []*pb.CreateAgentProviderRequest{
			{Slug: "", DisplayName: "x", BaseUrl: "http://x", Model: "m"},
			{Slug: "Bad Slug", DisplayName: "x", BaseUrl: "http://x", Model: "m"},
			{Slug: "ok", DisplayName: "", BaseUrl: "http://x", Model: "m"},
			{Slug: "ok", DisplayName: "x", BaseUrl: "", Model: "m"},
			{Slug: "ok", DisplayName: "x", BaseUrl: "http://x", Model: ""},
		}
		for _, req := range cases {
			_, err := CreateAgentProvider(ctx, req, r.Encryptor)
			assert.Equal(t, codes.InvalidArgument, status.Code(err), "req %+v", req)
		}
	})

	t.Run("rejects duplicate slug in the same org", func(t *testing.T) {
		req := &pb.CreateAgentProviderRequest{Slug: "dup", DisplayName: "D", BaseUrl: "http://x", Model: "m"}
		_, err := CreateAgentProvider(ctx, req, r.Encryptor)
		require.NoError(t, err)
		_, err = CreateAgentProvider(ctx, req, r.Encryptor)
		assert.Equal(t, codes.AlreadyExists, status.Code(err))
	})

	t.Run("requires authentication", func(t *testing.T) {
		_, err := CreateAgentProvider(context.Background(), &pb.CreateAgentProviderRequest{Slug: "x"}, r.Encryptor)
		assert.Equal(t, codes.Unauthenticated, status.Code(err))
	})
}

func TestAgentProviderLifecycle(t *testing.T) {
	r := support.Setup(t)
	ctx := authCtx(r.Organization.ID.String(), r.User.String())

	created, err := CreateAgentProvider(ctx, &pb.CreateAgentProviderRequest{
		Slug: "vllm", DisplayName: "vLLM", BaseUrl: "https://vllm.example.com/v1",
		Model: "qwen3-9b", ApiKey: "sec", Enabled: true,
	}, r.Encryptor)
	require.NoError(t, err)
	id := created.Provider.Id

	t.Run("list returns the org's providers", func(t *testing.T) {
		resp, err := ListAgentProviders(ctx)
		require.NoError(t, err)
		require.Len(t, resp.Providers, 1)
		assert.Equal(t, "vllm", resp.Providers[0].Slug)
	})

	t.Run("describe returns the provider; unknown id is NotFound", func(t *testing.T) {
		resp, err := DescribeAgentProvider(ctx, &pb.DescribeAgentProviderRequest{Id: id})
		require.NoError(t, err)
		assert.Equal(t, "vLLM", resp.Provider.DisplayName)

		_, err = DescribeAgentProvider(ctx, &pb.DescribeAgentProviderRequest{Id: uuid.NewString()})
		assert.Equal(t, codes.NotFound, status.Code(err))
	})

	t.Run("update changes fields and keeps the API key when omitted", func(t *testing.T) {
		resp, err := UpdateAgentProvider(ctx, &pb.UpdateAgentProviderRequest{
			Id: id, DisplayName: "vLLM v2", BaseUrl: "https://vllm2.example.com/v1",
			Model: "qwen3-32b", Enabled: false, ApiKey: "",
		}, r.Encryptor)
		require.NoError(t, err)
		assert.Equal(t, "vLLM v2", resp.Provider.DisplayName)
		assert.Equal(t, "https://vllm2.example.com/v1", resp.Provider.BaseUrl)
		assert.Equal(t, "qwen3-32b", resp.Provider.Model)
		assert.False(t, resp.Provider.Enabled)
		assert.True(t, resp.Provider.HasApiKey, "API key should be preserved when omitted")
	})

	t.Run("delete removes the provider", func(t *testing.T) {
		_, err := DeleteAgentProvider(ctx, &pb.DeleteAgentProviderRequest{Id: id})
		require.NoError(t, err)

		_, err = DescribeAgentProvider(ctx, &pb.DescribeAgentProviderRequest{Id: id})
		assert.Equal(t, codes.NotFound, status.Code(err))
	})
}
