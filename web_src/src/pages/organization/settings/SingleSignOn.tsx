import { Icon } from "@/components/Icon";
import { usePageTitle } from "@/hooks/usePageTitle";
import { PermissionTooltip } from "@/components/PermissionGate";
import { Link } from "@/components/Link/link";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/Table/table";
import { Button } from "@/components/ui/button";
import { LoadingButton } from "@/components/ui/loading-button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import { Switch } from "@/ui/switch";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { usePermissions } from "@/contexts/usePermissions";
import { getApiErrorMessage } from "@/lib/errors";
import { showErrorToast, showSuccessToast } from "@/lib/toast";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { KeyRound, Copy, Plus, X } from "lucide-react";
import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { useOidcProviders, useCreateOidcProvider, useDeleteOidcProvider } from "@/hooks/useOidcProviders";
import { IssuerVerifier } from "./components/IssuerVerifier";

interface SingleSignOnProps {
  organizationId: string;
}

const SLUG_PATTERN = /^[a-z0-9-]+$/;
const DEFAULT_SCOPES = "openid, email, profile";

const parseList = (value: string): string[] =>
  value
    .split(",")
    .map((item) => item.trim())
    .filter((item) => item.length > 0);

const ROLE_OPTIONS = [
  { label: "Admin", value: "org_admin" },
  { label: "Viewer", value: "org_viewer" },
] as const;

const DEFAULT_ROLE = "org_viewer";

interface GroupRoleRow {
  group: string;
  role: string;
}

const rowsToMapping = (rows: GroupRoleRow[]): { [group: string]: string } => {
  const mapping: { [group: string]: string } = {};
  for (const row of rows) {
    const group = row.group.trim();
    if (group.length > 0) {
      mapping[group] = row.role;
    }
  }
  return mapping;
};

const buildCallbackUrl = (organizationId: string, slug: string): string =>
  `${window.location.origin}/auth/sso/${organizationId}/${slug || "<slug>"}/callback`;

interface CreateFormValues {
  displayName: string;
  slug: string;
  issuerUrl: string;
  clientId: string;
  clientSecret: string;
}

const validateCreateForm = (values: CreateFormValues): string | null => {
  if (!values.displayName.trim()) return "Display name is required";
  if (!values.slug.trim()) return "Slug is required";
  if (!SLUG_PATTERN.test(values.slug.trim())) return "Slug may only contain lowercase letters, digits, and hyphens";
  if (!values.issuerUrl.trim()) return "Issuer URL is required";
  if (!values.clientId.trim()) return "Client ID is required";
  if (!values.clientSecret) return "Client secret is required";
  return null;
};

export function SingleSignOn({ organizationId }: SingleSignOnProps) {
  usePageTitle(["Single Sign-On"]);
  const navigate = useNavigate();
  const { canAct, isLoading: permissionsLoading } = usePermissions();
  const [isCreateModalOpen, setIsCreateModalOpen] = useState(false);
  const [slug, setSlug] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [type, setType] = useState("oidc");
  const [issuerUrl, setIssuerUrl] = useState("");
  const [clientId, setClientId] = useState("");
  const [clientSecret, setClientSecret] = useState("");
  const [scopes, setScopes] = useState(DEFAULT_SCOPES);
  const [allowedEmailDomains, setAllowedEmailDomains] = useState("");
  const [allowedGroups, setAllowedGroups] = useState("");
  const [groupRoleRows, setGroupRoleRows] = useState<GroupRoleRow[]>([]);
  const [groupsClaim, setGroupsClaim] = useState("");
  const [enabled, setEnabled] = useState(true);
  const canCreate = canAct("oidc_providers", "create");
  const canDelete = canAct("oidc_providers", "delete");

  const { data: providers = [], isLoading } = useOidcProviders(organizationId);
  const createMutation = useCreateOidcProvider(organizationId);
  const deleteMutation = useDeleteOidcProvider(organizationId);

  const resetForm = () => {
    setSlug("");
    setDisplayName("");
    setType("oidc");
    setIssuerUrl("");
    setClientId("");
    setClientSecret("");
    setScopes(DEFAULT_SCOPES);
    setAllowedEmailDomains("");
    setAllowedGroups("");
    setGroupRoleRows([]);
    setGroupsClaim("");
    setEnabled(true);
  };

  const handleAddGroupRoleRow = () => {
    setGroupRoleRows((rows) => [...rows, { group: "", role: DEFAULT_ROLE }]);
  };

  const handleRemoveGroupRoleRow = (index: number) => {
    setGroupRoleRows((rows) => rows.filter((_, i) => i !== index));
  };

  const handleGroupRoleRowChange = (index: number, updates: Partial<GroupRoleRow>) => {
    setGroupRoleRows((rows) => rows.map((row, i) => (i === index ? { ...row, ...updates } : row)));
  };

  const handleCreateClick = () => {
    if (!canCreate) return;
    resetForm();
    setIsCreateModalOpen(true);
  };

  const handleCloseCreateModal = () => {
    setIsCreateModalOpen(false);
    resetForm();
    createMutation.reset();
  };

  const handleCreate = async () => {
    if (!canCreate) return;
    const validationError = validateCreateForm({ displayName, slug, issuerUrl, clientId, clientSecret });
    if (validationError) {
      showErrorToast(validationError);
      return;
    }
    try {
      const result = await createMutation.mutateAsync({
        slug: slug.trim(),
        displayName: displayName.trim(),
        type,
        issuerUrl: issuerUrl.trim(),
        clientId: clientId.trim(),
        clientSecret,
        scopes: parseList(scopes),
        allowedEmailDomains: parseList(allowedEmailDomains),
        allowedGroups: parseList(allowedGroups),
        groupsClaim: groupsClaim.trim(),
        groupRoleMappings: rowsToMapping(groupRoleRows),
        enabled,
      });
      showSuccessToast("Provider created");
      const providerId = result.data?.provider?.id;
      handleCloseCreateModal();
      if (providerId) {
        navigate(`/${organizationId}/settings/sso/${providerId}`);
      }
    } catch (error) {
      showErrorToast(`Failed to create provider: ${getApiErrorMessage(error)}`);
    }
  };

  const handleCopyCallbackUrl = async () => {
    try {
      await navigator.clipboard.writeText(buildCallbackUrl(organizationId, slug.trim()));
      showSuccessToast("Callback URL copied to clipboard");
    } catch {
      showErrorToast("Failed to copy callback URL");
    }
  };

  const handleDelete = async (id: string, name: string) => {
    if (!canDelete) return;
    if (!confirm(`Are you sure you want to delete provider "${name}"? This cannot be undone.`)) return;
    try {
      await deleteMutation.mutateAsync(id);
      showSuccessToast("Provider deleted");
    } catch (error) {
      showErrorToast(`Failed to delete: ${getApiErrorMessage(error)}`);
    }
  };

  const getDetailPath = (id: string) => `/${organizationId}/settings/sso/${id}`;

  if (isLoading) {
    return (
      <div className="space-y-6 pt-6">
        <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-300 dark:border-gray-800 overflow-hidden">
          <div className="px-6 pb-6 min-h-96 flex justify-center items-center">
            <p className="text-gray-500 dark:text-gray-400">Loading providers...</p>
          </div>
        </div>
      </div>
    );
  }

  const sorted = [...providers].sort((a, b) => (a.displayName || "").localeCompare(b.displayName || ""));

  return (
    <div className="space-y-6 pt-6">
      <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-300 dark:border-gray-800 overflow-hidden">
        {sorted.length > 0 && (
          <div className="px-6 pt-6 pb-4 flex items-center justify-start">
            <PermissionTooltip
              allowed={canCreate || permissionsLoading}
              message="You don't have permission to create SSO providers."
            >
              <Button
                className="flex items-center"
                onClick={handleCreateClick}
                disabled={!canCreate}
                data-testid="sso-create-btn"
              >
                <Icon name="plus" />
                Add provider
              </Button>
            </PermissionTooltip>
          </div>
        )}
        <div className="px-6 pb-6 min-h-96">
          {sorted.length === 0 ? (
            <div className="flex min-h-96 flex-col items-center justify-center text-center">
              <div className="flex justify-center items-center text-gray-800">
                <KeyRound size={32} />
              </div>
              <p className="mt-3 text-sm text-gray-800">Add your first SSO provider</p>
              <p className="mt-1 text-xs text-gray-500">
                Let members sign in to this organization through your identity provider.
              </p>
              <PermissionTooltip
                allowed={canCreate || permissionsLoading}
                message="You don't have permission to create SSO providers."
              >
                <Button
                  className="mt-4 flex items-center"
                  onClick={handleCreateClick}
                  disabled={!canCreate}
                  data-testid="sso-create-btn"
                >
                  <Icon name="plus" />
                  Add provider
                </Button>
              </PermissionTooltip>
            </div>
          ) : (
            <Table dense>
              <TableHead>
                <TableRow>
                  <TableHeader>Name</TableHeader>
                  <TableHeader>Slug</TableHeader>
                  <TableHeader>Issuer URL</TableHeader>
                  <TableHeader>Status</TableHeader>
                  <TableHeader></TableHeader>
                </TableRow>
              </TableHead>
              <TableBody>
                {sorted.map((provider) => (
                  <TableRow key={provider.id} className="last:[&>td]:border-b-0">
                    <TableCell>
                      <div className="flex items-center gap-2">
                        <KeyRound size={16} className="text-gray-500" />
                        <Link
                          href={getDetailPath(provider.id || "")}
                          className="cursor-pointer text-sm !font-semibold text-gray-800 !underline underline-offset-2"
                          data-testid="sso-link"
                        >
                          {provider.displayName || "Unnamed"}
                        </Link>
                      </div>
                    </TableCell>
                    <TableCell>
                      <span className="text-sm font-mono text-gray-500 dark:text-gray-400">{provider.slug || "—"}</span>
                    </TableCell>
                    <TableCell>
                      <span className="text-sm text-gray-500 dark:text-gray-400">{provider.issuerUrl || "—"}</span>
                    </TableCell>
                    <TableCell>
                      <Badge variant={provider.enabled ? "default" : "secondary"}>
                        {provider.enabled ? "Enabled" : "Disabled"}
                      </Badge>
                    </TableCell>
                    <TableCell>
                      <div className="flex justify-end">
                        <PermissionTooltip
                          allowed={canDelete || permissionsLoading}
                          message="You don't have permission to delete SSO providers."
                        >
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => handleDelete(provider.id || "", provider.displayName || "")}
                            disabled={!canDelete || deleteMutation.isPending}
                            className="text-red-600 hover:text-red-700"
                            data-testid="sso-delete-btn"
                          >
                            <Icon name="trash-2" size="sm" />
                          </Button>
                        </PermissionTooltip>
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </div>
      </div>

      {/* Create modal */}
      {isCreateModalOpen && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50">
          <div className="bg-white dark:bg-gray-900 rounded-lg shadow-xl max-w-lg w-full mx-4 max-h-[90vh] overflow-y-auto">
            <form
              className="p-6"
              onSubmit={(e) => {
                e.preventDefault();
                handleCreate();
              }}
              data-testid="sso-create-form"
            >
              <div className="flex items-center justify-between mb-6">
                <div className="flex items-center gap-3">
                  <KeyRound className="w-6 h-6 text-gray-500 dark:text-gray-400" />
                  <h3 className="text-base font-semibold text-gray-800 dark:text-gray-100">Add SSO provider</h3>
                </div>
                <button
                  type="button"
                  onClick={handleCloseCreateModal}
                  className="text-gray-500 hover:text-gray-800 dark:hover:text-gray-300"
                  disabled={createMutation.isPending}
                >
                  <Icon name="x" size="sm" />
                </button>
              </div>

              <div className="space-y-4">
                <div>
                  <Label className="text-gray-800 dark:text-gray-100 mb-2">
                    Display Name <span className="text-red-500">*</span>
                  </Label>
                  <Input
                    type="text"
                    value={displayName}
                    onChange={(e) => setDisplayName(e.target.value)}
                    placeholder="e.g., Acme Okta"
                    required
                    data-testid="sso-create-display-name"
                  />
                </div>
                <div>
                  <Label className="text-gray-800 dark:text-gray-100 mb-2">
                    Slug <span className="text-red-500">*</span>
                  </Label>
                  <Input
                    type="text"
                    value={slug}
                    onChange={(e) => setSlug(e.target.value)}
                    placeholder="e.g., acme-okta"
                    required
                    data-testid="sso-create-slug"
                  />
                  <p className="mt-1 text-xs text-gray-500">
                    Used in the login URL. Lowercase letters, digits, and hyphens only.
                  </p>
                </div>
                <div>
                  <Label className="text-gray-800 dark:text-gray-100 mb-2">Type</Label>
                  <Select value={type} onValueChange={setType}>
                    <SelectTrigger className="w-full" data-testid="sso-create-type">
                      <SelectValue placeholder="Select a type" />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="oidc">OpenID Connect</SelectItem>
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <div>
                            <SelectItem value="saml" disabled>
                              SAML 2.0
                            </SelectItem>
                          </div>
                        </TooltipTrigger>
                        <TooltipContent>Not yet supported</TooltipContent>
                      </Tooltip>
                    </SelectContent>
                  </Select>
                </div>
                <div>
                  <Label className="text-gray-800 dark:text-gray-100 mb-2">
                    Issuer URL <span className="text-red-500">*</span>
                  </Label>
                  <Input
                    type="text"
                    value={issuerUrl}
                    onChange={(e) => setIssuerUrl(e.target.value)}
                    placeholder="https://idp.example.com"
                    required
                    data-testid="sso-create-issuer-url"
                  />
                  <IssuerVerifier
                    issuerUrl={issuerUrl}
                    organizationId={organizationId}
                    onScopesDiscovered={setScopes}
                  />
                </div>
                <div>
                  <Label className="text-gray-800 dark:text-gray-100 mb-2">
                    Client ID <span className="text-red-500">*</span>
                  </Label>
                  <Input
                    type="text"
                    value={clientId}
                    onChange={(e) => setClientId(e.target.value)}
                    required
                    data-testid="sso-create-client-id"
                  />
                </div>
                <div>
                  <Label className="text-gray-800 dark:text-gray-100 mb-2">
                    Client Secret <span className="text-red-500">*</span>
                  </Label>
                  <Input
                    type="password"
                    value={clientSecret}
                    onChange={(e) => setClientSecret(e.target.value)}
                    required
                    className="ph-no-capture"
                    data-testid="sso-create-client-secret"
                  />
                </div>
                <div>
                  <Label className="text-gray-800 dark:text-gray-100 mb-2">Scopes</Label>
                  <Input
                    type="text"
                    value={scopes}
                    onChange={(e) => setScopes(e.target.value)}
                    placeholder={DEFAULT_SCOPES}
                    data-testid="sso-create-scopes"
                  />
                  <p className="mt-1 text-xs text-gray-500">Comma-separated list of OIDC scopes.</p>
                </div>
                <div>
                  <Label className="text-gray-800 dark:text-gray-100 mb-2">Allowed Email Domains</Label>
                  <Input
                    type="text"
                    value={allowedEmailDomains}
                    onChange={(e) => setAllowedEmailDomains(e.target.value)}
                    placeholder="e.g., example.com, example.org"
                    data-testid="sso-create-allowed-email-domains"
                  />
                  <p className="mt-1 text-xs text-gray-500">
                    Comma-separated. Restricts which emails may use this provider and enables discovery from the login
                    page.
                  </p>
                </div>
                <div>
                  <Label className="text-gray-800 dark:text-gray-100 mb-2">Allowed Groups</Label>
                  <Input
                    type="text"
                    value={allowedGroups}
                    onChange={(e) => setAllowedGroups(e.target.value)}
                    placeholder="e.g., engineering, platform"
                    data-testid="sso-create-allowed-groups"
                  />
                  <p className="mt-1 text-xs text-gray-500">
                    If set, only users who are a member of at least one of these IdP groups may sign in. Leave empty to
                    allow any user (subject to allowed domains). Requires your IdP to emit a <code>groups</code> claim.
                  </p>
                </div>
                <div>
                  <Label className="text-gray-800 dark:text-gray-100 mb-2">Group → Role mapping</Label>
                  <div className="space-y-2" data-testid="sso-create-group-role-mappings">
                    {groupRoleRows.map((row, index) => (
                      <div key={index} className="flex items-center gap-2">
                        <Input
                          type="text"
                          value={row.group}
                          onChange={(e) => handleGroupRoleRowChange(index, { group: e.target.value })}
                          placeholder="IdP group name"
                          className="flex-1"
                          data-testid={`sso-create-group-role-group-${index}`}
                        />
                        <Select
                          value={row.role}
                          onValueChange={(value) => handleGroupRoleRowChange(index, { role: value })}
                        >
                          <SelectTrigger className="w-32" data-testid={`sso-create-group-role-role-${index}`}>
                            <SelectValue />
                          </SelectTrigger>
                          <SelectContent>
                            {ROLE_OPTIONS.map((option) => (
                              <SelectItem key={option.value} value={option.value}>
                                {option.label}
                              </SelectItem>
                            ))}
                          </SelectContent>
                        </Select>
                        <Button
                          type="button"
                          variant="ghost"
                          size="sm"
                          onClick={() => handleRemoveGroupRoleRow(index)}
                          className="text-gray-500 hover:text-gray-800"
                          aria-label="Remove mapping"
                          data-testid={`sso-create-group-role-remove-${index}`}
                        >
                          <X className="w-4 h-4" />
                        </Button>
                      </div>
                    ))}
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      onClick={handleAddGroupRoleRow}
                      className="flex items-center gap-1"
                      data-testid="sso-create-group-role-add"
                    >
                      <Plus className="w-4 h-4" />
                      Add mapping
                    </Button>
                  </div>
                  <p className="mt-1 text-xs text-gray-500">
                    Users in a mapped group are granted that role on every login — the IdP is authoritative and
                    overrides manual role changes. Unmapped users get Viewer. An organization Owner is never demoted by
                    this.
                  </p>
                </div>
                <div>
                  <Label className="text-gray-800 dark:text-gray-100 mb-2">Groups claim</Label>
                  <Input
                    type="text"
                    value={groupsClaim}
                    onChange={(e) => setGroupsClaim(e.target.value)}
                    placeholder="groups"
                    data-testid="sso-create-groups-claim"
                  />
                  <p className="mt-1 text-xs text-gray-500">
                    The ID-token claim to read group membership from. Defaults to <code>groups</code>. Set this for IdPs
                    such as Okta or Entra ID that emit groups under a different claim name — and request the matching
                    scope yourself in Scopes.
                  </p>
                </div>
                <div className="flex items-center gap-3">
                  <Switch checked={enabled} onCheckedChange={setEnabled} data-testid="sso-create-enabled" />
                  <Label className="text-gray-800 dark:text-gray-100">Enabled</Label>
                </div>
                <div>
                  <Label className="text-gray-800 dark:text-gray-100 mb-2">Callback URL</Label>
                  <div className="flex items-center gap-2">
                    <Input
                      readOnly
                      value={buildCallbackUrl(organizationId, slug.trim())}
                      className="flex-1 font-mono text-xs bg-gray-50 dark:bg-gray-800"
                      data-testid="sso-create-callback-url"
                    />
                    <Button type="button" variant="outline" onClick={handleCopyCallbackUrl}>
                      <Copy className="w-4 h-4" />
                    </Button>
                  </div>
                  <p className="mt-1 text-xs text-gray-500">Register this redirect URI in your identity provider.</p>
                </div>
              </div>

              <div className="flex justify-start gap-3 mt-6">
                <LoadingButton
                  type="submit"
                  disabled={!displayName?.trim() || !slug?.trim()}
                  loading={createMutation.isPending}
                  loadingText="Creating..."
                  className="flex items-center gap-2"
                  data-testid="sso-create-submit"
                >
                  Create
                </LoadingButton>
                <Button
                  type="button"
                  variant="outline"
                  onClick={handleCloseCreateModal}
                  disabled={createMutation.isPending}
                >
                  Cancel
                </Button>
              </div>

              {createMutation.isError && (
                <div className="mt-4 p-3 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md">
                  <p className="text-sm text-red-800 dark:text-red-200">
                    Failed to create: {getApiErrorMessage(createMutation.error)}
                  </p>
                </div>
              )}
            </form>
          </div>
        </div>
      )}
    </div>
  );
}
