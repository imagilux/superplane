import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import {
  agentProvidersListAgentProviders,
  agentProvidersCreateAgentProvider,
  agentProvidersDescribeAgentProvider,
  agentProvidersUpdateAgentProvider,
  agentProvidersDeleteAgentProvider,
} from "@/api-client/sdk.gen";
import { withOrganizationHeader } from "@/lib/withOrganizationHeader";

export const agentProviderKeys = {
  all: ["agentProviders"] as const,
  list: (orgId: string) => [...agentProviderKeys.all, "list", orgId] as const,
  detail: (orgId: string, id: string) => [...agentProviderKeys.all, "detail", orgId, id] as const,
};

export const useAgentProviders = (organizationId: string) => {
  return useQuery({
    queryKey: agentProviderKeys.list(organizationId),
    queryFn: async () => {
      const response = await agentProvidersListAgentProviders(withOrganizationHeader({}));
      return response.data?.providers || [];
    },
    staleTime: 2 * 60 * 1000,
    gcTime: 5 * 60 * 1000,
    enabled: !!organizationId,
  });
};

export const useAgentProvider = (organizationId: string, id: string) => {
  return useQuery({
    queryKey: agentProviderKeys.detail(organizationId, id),
    queryFn: async () => {
      const response = await agentProvidersDescribeAgentProvider(
        withOrganizationHeader({
          path: { id },
        }),
      );
      return response.data?.provider || null;
    },
    staleTime: 2 * 60 * 1000,
    gcTime: 5 * 60 * 1000,
    enabled: !!organizationId && !!id,
  });
};

export const useCreateAgentProvider = (organizationId: string) => {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async (params: {
      slug: string;
      displayName: string;
      type: string;
      baseUrl: string;
      model: string;
      apiKey: string;
      enabled: boolean;
    }) => {
      return agentProvidersCreateAgentProvider(
        withOrganizationHeader({
          body: {
            slug: params.slug,
            displayName: params.displayName,
            type: params.type,
            baseUrl: params.baseUrl,
            model: params.model,
            apiKey: params.apiKey,
            enabled: params.enabled,
          },
        }),
      );
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: agentProviderKeys.list(organizationId) });
    },
  });
};

export const useUpdateAgentProvider = (organizationId: string) => {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async (params: {
      id: string;
      displayName: string;
      baseUrl: string;
      model: string;
      apiKey: string;
      enabled: boolean;
    }) => {
      return agentProvidersUpdateAgentProvider(
        withOrganizationHeader({
          path: { id: params.id },
          body: {
            displayName: params.displayName,
            baseUrl: params.baseUrl,
            model: params.model,
            apiKey: params.apiKey,
            enabled: params.enabled,
          },
        }),
      );
    },
    onSuccess: (_data, variables) => {
      queryClient.invalidateQueries({ queryKey: agentProviderKeys.list(organizationId) });
      queryClient.invalidateQueries({ queryKey: agentProviderKeys.detail(organizationId, variables.id) });
    },
  });
};

export const useDeleteAgentProvider = (organizationId: string) => {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async (id: string) => {
      return agentProvidersDeleteAgentProvider(
        withOrganizationHeader({
          path: { id },
        }),
      );
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: agentProviderKeys.list(organizationId) });
    },
  });
};
