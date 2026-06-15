import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import {
  oidcProvidersListOidcProviders,
  oidcProvidersCreateOidcProvider,
  oidcProvidersDescribeOidcProvider,
  oidcProvidersUpdateOidcProvider,
  oidcProvidersDeleteOidcProvider,
  oidcProvidersDiscoverOidcProvider,
} from "@/api-client/sdk.gen";
import type { OidcProvidersDiscoverOidcProviderResponse } from "@/api-client/types.gen";
import { withOrganizationHeader } from "@/lib/withOrganizationHeader";

export const oidcProviderKeys = {
  all: ["oidcProviders"] as const,
  list: (orgId: string) => [...oidcProviderKeys.all, "list", orgId] as const,
  detail: (orgId: string, id: string) => [...oidcProviderKeys.all, "detail", orgId, id] as const,
};

export const useOidcProviders = (organizationId: string) => {
  return useQuery({
    queryKey: oidcProviderKeys.list(organizationId),
    queryFn: async () => {
      const response = await oidcProvidersListOidcProviders(withOrganizationHeader({}));
      return response.data?.providers || [];
    },
    staleTime: 2 * 60 * 1000,
    gcTime: 5 * 60 * 1000,
    enabled: !!organizationId,
  });
};

export const useOidcProvider = (organizationId: string, id: string) => {
  return useQuery({
    queryKey: oidcProviderKeys.detail(organizationId, id),
    queryFn: async () => {
      const response = await oidcProvidersDescribeOidcProvider(
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

export const useCreateOidcProvider = (organizationId: string) => {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async (params: {
      slug: string;
      displayName: string;
      type: string;
      issuerUrl: string;
      clientId: string;
      clientSecret: string;
      scopes: string[];
      allowedEmailDomains: string[];
      allowedGroups: string[];
      groupRoleMappings: { [group: string]: string };
      enabled: boolean;
    }) => {
      return oidcProvidersCreateOidcProvider(
        withOrganizationHeader({
          body: {
            slug: params.slug,
            displayName: params.displayName,
            type: params.type,
            issuerUrl: params.issuerUrl,
            clientId: params.clientId,
            clientSecret: params.clientSecret,
            scopes: params.scopes,
            allowedEmailDomains: params.allowedEmailDomains,
            allowedGroups: params.allowedGroups,
            groupRoleMappings: params.groupRoleMappings,
            enabled: params.enabled,
          },
        }),
      );
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: oidcProviderKeys.list(organizationId) });
    },
  });
};

export const useUpdateOidcProvider = (organizationId: string) => {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async (params: {
      id: string;
      displayName: string;
      issuerUrl: string;
      clientId: string;
      clientSecret: string;
      scopes: string[];
      allowedEmailDomains: string[];
      allowedGroups: string[];
      groupRoleMappings: { [group: string]: string };
      enabled: boolean;
    }) => {
      return oidcProvidersUpdateOidcProvider(
        withOrganizationHeader({
          path: { id: params.id },
          body: {
            displayName: params.displayName,
            issuerUrl: params.issuerUrl,
            clientId: params.clientId,
            clientSecret: params.clientSecret,
            scopes: params.scopes,
            allowedEmailDomains: params.allowedEmailDomains,
            allowedGroups: params.allowedGroups,
            groupRoleMappings: params.groupRoleMappings,
            enabled: params.enabled,
          },
        }),
      );
    },
    onSuccess: (_data, variables) => {
      queryClient.invalidateQueries({ queryKey: oidcProviderKeys.list(organizationId) });
      queryClient.invalidateQueries({ queryKey: oidcProviderKeys.detail(organizationId, variables.id) });
    },
  });
};

export const useDeleteOidcProvider = (organizationId: string) => {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async (id: string) => {
      return oidcProvidersDeleteOidcProvider(
        withOrganizationHeader({
          path: { id },
        }),
      );
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: oidcProviderKeys.list(organizationId) });
    },
  });
};

export const useDiscoverOidcProvider = (organizationId: string) => {
  return useMutation<OidcProvidersDiscoverOidcProviderResponse, Error, string>({
    mutationFn: async (issuerUrl: string) => {
      const response = await oidcProvidersDiscoverOidcProvider(
        withOrganizationHeader({
          organizationId,
          body: { issuerUrl },
        }),
      );
      return response.data ?? {};
    },
  });
};
