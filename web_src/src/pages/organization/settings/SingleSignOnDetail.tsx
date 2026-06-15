import { Icon } from "@/components/Icon";
import { usePageTitle } from "@/hooks/usePageTitle";
import { PermissionTooltip } from "@/components/PermissionGate";
import { Button } from "@/components/ui/button";
import { LoadingButton } from "@/components/ui/loading-button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import { Switch } from "@/ui/switch";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { usePermissions } from "@/contexts/usePermissions";
import { getApiErrorMessage } from "@/lib/errors";
import { showErrorToast, showSuccessToast } from "@/lib/toast";
import { KeyRound, Copy, ArrowLeft, Plus, X } from "lucide-react";
import { useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useOidcProvider, useUpdateOidcProvider, useDeleteOidcProvider } from "@/hooks/useOidcProviders";
import { IssuerVerifier } from "./components/IssuerVerifier";

interface SingleSignOnDetailProps {
  organizationId: string;
}

const parseList = (value: string): string[] =>
  value
    .split(",")
    .map((item) => item.trim())
    .filter((item) => item.length > 0);

const buildCallbackUrl = (organizationId: string, slug: string): string =>
  `${window.location.origin}/auth/sso/${organizationId}/${slug || "<slug>"}/callback`;

const formatList = (items: string[] | undefined): string => (items && items.length > 0 ? items.join(", ") : "—");

const ROLE_OPTIONS = [
  { label: "Admin", value: "org_admin" },
  { label: "Viewer", value: "org_viewer" },
] as const;

const DEFAULT_ROLE = "org_viewer";

interface GroupRoleRow {
  group: string;
  role: string;
}

const mappingToRows = (mapping: { [group: string]: string } | undefined): GroupRoleRow[] =>
  Object.entries(mapping || {}).map(([group, role]) => ({ group, role }));

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

const formatMapping = (mapping: { [group: string]: string } | undefined): string => {
  const entries = Object.entries(mapping || {});
  if (entries.length === 0) return "—";
  const roleLabel = (role: string) => ROLE_OPTIONS.find((option) => option.value === role)?.label || role;
  return entries.map(([group, role]) => `${group} → ${roleLabel(role)}`).join(", ");
};

const validateEditForm = (displayName: string, issuerUrl: string, clientId: string): string | null => {
  if (!displayName.trim()) return "Display name is required";
  if (!issuerUrl.trim()) return "Issuer URL is required";
  if (!clientId.trim()) return "Client ID is required";
  return null;
};

export function SingleSignOnDetail({ organizationId }: SingleSignOnDetailProps) {
  const navigate = useNavigate();
  const { id } = useParams<{ id: string }>();
  const { canAct, isLoading: permissionsLoading } = usePermissions();

  const { data: provider, isLoading } = useOidcProvider(organizationId, id || "");
  usePageTitle(["Single Sign-On", provider?.displayName]);
  const canUpdate = canAct("oidc_providers", "update");
  const canDelete = canAct("oidc_providers", "delete");

  const updateMutation = useUpdateOidcProvider(organizationId);
  const deleteMutation = useDeleteOidcProvider(organizationId);

  const [isEditing, setIsEditing] = useState(false);
  const [editDisplayName, setEditDisplayName] = useState("");
  const [editIssuerUrl, setEditIssuerUrl] = useState("");
  const [editClientId, setEditClientId] = useState("");
  const [editClientSecret, setEditClientSecret] = useState("");
  const [editScopes, setEditScopes] = useState("");
  const [editAllowedEmailDomains, setEditAllowedEmailDomains] = useState("");
  const [editAllowedGroups, setEditAllowedGroups] = useState("");
  const [editGroupRoleRows, setEditGroupRoleRows] = useState<GroupRoleRow[]>([]);
  const [editEnabled, setEditEnabled] = useState(true);

  const handleEditStart = () => {
    setEditDisplayName(provider?.displayName || "");
    setEditIssuerUrl(provider?.issuerUrl || "");
    setEditClientId(provider?.clientId || "");
    setEditClientSecret("");
    setEditScopes((provider?.scopes || []).join(", "));
    setEditAllowedEmailDomains((provider?.allowedEmailDomains || []).join(", "));
    setEditAllowedGroups((provider?.allowedGroups || []).join(", "));
    setEditGroupRoleRows(mappingToRows(provider?.groupRoleMappings));
    setEditEnabled(provider?.enabled ?? true);
    setIsEditing(true);
  };

  const handleAddGroupRoleRow = () => {
    setEditGroupRoleRows((rows) => [...rows, { group: "", role: DEFAULT_ROLE }]);
  };

  const handleRemoveGroupRoleRow = (index: number) => {
    setEditGroupRoleRows((rows) => rows.filter((_, i) => i !== index));
  };

  const handleGroupRoleRowChange = (index: number, updates: Partial<GroupRoleRow>) => {
    setEditGroupRoleRows((rows) => rows.map((row, i) => (i === index ? { ...row, ...updates } : row)));
  };

  const handleEditCancel = () => {
    setIsEditing(false);
  };

  const handleEditSave = async () => {
    if (!canUpdate || !id) return;
    const validationError = validateEditForm(editDisplayName, editIssuerUrl, editClientId);
    if (validationError) {
      showErrorToast(validationError);
      return;
    }
    try {
      await updateMutation.mutateAsync({
        id,
        displayName: editDisplayName.trim(),
        issuerUrl: editIssuerUrl.trim(),
        clientId: editClientId.trim(),
        clientSecret: editClientSecret,
        scopes: parseList(editScopes),
        allowedEmailDomains: parseList(editAllowedEmailDomains),
        allowedGroups: parseList(editAllowedGroups),
        groupRoleMappings: rowsToMapping(editGroupRoleRows),
        enabled: editEnabled,
      });
      showSuccessToast("Provider updated");
      setIsEditing(false);
    } catch (error) {
      showErrorToast(`Failed to update: ${getApiErrorMessage(error)}`);
    }
  };

  const handleDelete = async () => {
    if (!canDelete || !id) return;
    if (!confirm(`Are you sure you want to delete "${provider?.displayName}"? This cannot be undone.`)) return;
    try {
      await deleteMutation.mutateAsync(id);
      showSuccessToast("Provider deleted");
      navigate(`/${organizationId}/settings/sso`);
    } catch (error) {
      showErrorToast(`Failed to delete: ${getApiErrorMessage(error)}`);
    }
  };

  const handleCopyCallbackUrl = async () => {
    if (!provider) return;
    try {
      await navigator.clipboard.writeText(buildCallbackUrl(organizationId, provider.slug || ""));
      showSuccessToast("Callback URL copied to clipboard");
    } catch {
      showErrorToast("Failed to copy callback URL");
    }
  };

  if (isLoading) {
    return (
      <div className="space-y-6 pt-6">
        <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-300 dark:border-gray-800 overflow-hidden">
          <div className="px-6 pb-6 min-h-96 flex justify-center items-center">
            <p className="text-gray-500 dark:text-gray-400">Loading...</p>
          </div>
        </div>
      </div>
    );
  }

  if (!provider) {
    return (
      <div className="space-y-6 pt-6">
        <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-300 dark:border-gray-800 overflow-hidden">
          <div className="px-6 pb-6 min-h-96 flex justify-center items-center">
            <p className="text-gray-500 dark:text-gray-400">Provider not found</p>
          </div>
        </div>
      </div>
    );
  }

  const createdAt = provider.createdAt ? new Date(provider.createdAt).toLocaleDateString() : "—";
  const providersHref = `/${organizationId}/settings/sso`;
  const callbackUrl = buildCallbackUrl(organizationId, provider.slug || "");
  const typeLabel = provider.type === "saml" ? "SAML 2.0" : "OpenID Connect";

  return (
    <div className="space-y-6 pt-6">
      {/* Back button */}
      <Link
        to={providersHref}
        className="flex items-center gap-1 text-sm text-gray-500 hover:text-gray-800 transition"
        aria-label="Back to single sign-on"
      >
        <ArrowLeft size={14} />
        Back to single sign-on
      </Link>

      {/* Details */}
      <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-300 dark:border-gray-800 overflow-hidden">
        <div className="px-6 py-6">
          <div className="flex items-center justify-between mb-6">
            <div className="flex items-center gap-3">
              <KeyRound size={20} className="text-gray-500" />
              <h2 className="text-lg font-semibold text-gray-800 dark:text-white">{provider.displayName}</h2>
              <Badge variant={provider.enabled ? "default" : "secondary"}>
                {provider.enabled ? "Enabled" : "Disabled"}
              </Badge>
            </div>
            <div className="flex gap-2">
              {!isEditing && (
                <PermissionTooltip
                  allowed={canUpdate || permissionsLoading}
                  message="You don't have permission to update SSO providers."
                >
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={handleEditStart}
                    disabled={!canUpdate}
                    data-testid="sso-detail-edit"
                  >
                    <Icon name="pencil" size="sm" />
                    Edit
                  </Button>
                </PermissionTooltip>
              )}
              <PermissionTooltip
                allowed={canDelete || permissionsLoading}
                message="You don't have permission to delete SSO providers."
              >
                <Button
                  variant="outline"
                  size="sm"
                  onClick={handleDelete}
                  disabled={!canDelete || deleteMutation.isPending}
                  className="text-red-600 hover:text-red-700"
                  data-testid="sso-detail-delete"
                >
                  <Icon name="trash-2" size="sm" />
                  Delete
                </Button>
              </PermissionTooltip>
            </div>
          </div>

          {isEditing ? (
            <form
              className="space-y-4"
              onSubmit={(e) => {
                e.preventDefault();
                handleEditSave();
              }}
            >
              <div>
                <Label className="text-gray-800 dark:text-gray-100 mb-2">
                  Display Name <span className="text-red-500">*</span>
                </Label>
                <Input
                  type="text"
                  value={editDisplayName}
                  onChange={(e) => setEditDisplayName(e.target.value)}
                  required
                  data-testid="sso-detail-edit-display-name"
                />
              </div>
              <div>
                <Label className="text-gray-800 dark:text-gray-100 mb-2">
                  Issuer URL <span className="text-red-500">*</span>
                </Label>
                <Input
                  type="text"
                  value={editIssuerUrl}
                  onChange={(e) => setEditIssuerUrl(e.target.value)}
                  required
                  data-testid="sso-detail-edit-issuer-url"
                />
                <IssuerVerifier
                  issuerUrl={editIssuerUrl}
                  organizationId={organizationId}
                  onScopesDiscovered={setEditScopes}
                />
              </div>
              <div>
                <Label className="text-gray-800 dark:text-gray-100 mb-2">
                  Client ID <span className="text-red-500">*</span>
                </Label>
                <Input
                  type="text"
                  value={editClientId}
                  onChange={(e) => setEditClientId(e.target.value)}
                  required
                  data-testid="sso-detail-edit-client-id"
                />
              </div>
              <div>
                <Label className="text-gray-800 dark:text-gray-100 mb-2">Replace secret</Label>
                <Input
                  type="password"
                  value={editClientSecret}
                  onChange={(e) => setEditClientSecret(e.target.value)}
                  placeholder="leave blank to keep current"
                  className="ph-no-capture"
                  data-testid="sso-detail-edit-client-secret"
                />
              </div>
              <div>
                <Label className="text-gray-800 dark:text-gray-100 mb-2">Scopes</Label>
                <Input
                  type="text"
                  value={editScopes}
                  onChange={(e) => setEditScopes(e.target.value)}
                  placeholder="openid, email, profile"
                  data-testid="sso-detail-edit-scopes"
                />
                <p className="mt-1 text-xs text-gray-500">Comma-separated list of OIDC scopes.</p>
              </div>
              <div>
                <Label className="text-gray-800 dark:text-gray-100 mb-2">Allowed Email Domains</Label>
                <Input
                  type="text"
                  value={editAllowedEmailDomains}
                  onChange={(e) => setEditAllowedEmailDomains(e.target.value)}
                  placeholder="e.g., example.com, example.org"
                  data-testid="sso-detail-edit-allowed-email-domains"
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
                  value={editAllowedGroups}
                  onChange={(e) => setEditAllowedGroups(e.target.value)}
                  placeholder="e.g., engineering, platform"
                  data-testid="sso-detail-edit-allowed-groups"
                />
                <p className="mt-1 text-xs text-gray-500">
                  If set, only users who are a member of at least one of these IdP groups may sign in. Leave empty to
                  allow any user (subject to allowed domains). Requires your IdP to emit a <code>groups</code> claim.
                </p>
              </div>
              <div>
                <Label className="text-gray-800 dark:text-gray-100 mb-2">Group → Role mapping</Label>
                <div className="space-y-2" data-testid="sso-detail-edit-group-role-mappings">
                  {editGroupRoleRows.map((row, index) => (
                    <div key={index} className="flex items-center gap-2">
                      <Input
                        type="text"
                        value={row.group}
                        onChange={(e) => handleGroupRoleRowChange(index, { group: e.target.value })}
                        placeholder="IdP group name"
                        className="flex-1"
                        data-testid={`sso-detail-edit-group-role-group-${index}`}
                      />
                      <Select
                        value={row.role}
                        onValueChange={(value) => handleGroupRoleRowChange(index, { role: value })}
                      >
                        <SelectTrigger className="w-32" data-testid={`sso-detail-edit-group-role-role-${index}`}>
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
                        data-testid={`sso-detail-edit-group-role-remove-${index}`}
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
                    data-testid="sso-detail-edit-group-role-add"
                  >
                    <Plus className="w-4 h-4" />
                    Add mapping
                  </Button>
                </div>
                <p className="mt-1 text-xs text-gray-500">
                  Users in a mapped group are granted that role on every login — the IdP is authoritative and overrides
                  manual role changes. Unmapped users get Viewer. An organization Owner is never demoted by this.
                </p>
              </div>
              <div className="flex items-center gap-3">
                <Switch checked={editEnabled} onCheckedChange={setEditEnabled} data-testid="sso-detail-edit-enabled" />
                <Label className="text-gray-800 dark:text-gray-100">Enabled</Label>
              </div>
              <div className="flex gap-2">
                <LoadingButton
                  type="submit"
                  disabled={!editDisplayName?.trim()}
                  loading={updateMutation.isPending}
                  loadingText="Saving..."
                  className="flex items-center gap-2"
                >
                  Save
                </LoadingButton>
                <Button type="button" variant="outline" onClick={handleEditCancel} disabled={updateMutation.isPending}>
                  Cancel
                </Button>
              </div>
            </form>
          ) : (
            <dl className="grid grid-cols-2 gap-y-4 text-sm">
              <dt className="text-gray-500 dark:text-gray-400">Slug</dt>
              <dd className="text-gray-800 dark:text-white font-mono text-xs">{provider.slug || "—"}</dd>
              <dt className="text-gray-500 dark:text-gray-400">Type</dt>
              <dd className="text-gray-800 dark:text-white">{typeLabel}</dd>
              <dt className="text-gray-500 dark:text-gray-400">Issuer URL</dt>
              <dd className="text-gray-800 dark:text-white break-all">{provider.issuerUrl || "—"}</dd>
              <dt className="text-gray-500 dark:text-gray-400">Client ID</dt>
              <dd className="text-gray-800 dark:text-white font-mono text-xs break-all">{provider.clientId || "—"}</dd>
              <dt className="text-gray-500 dark:text-gray-400">Client Secret</dt>
              <dd className="text-gray-800 dark:text-white">
                {provider.hasClientSecret ? (
                  <span className="text-green-600 dark:text-green-400">Configured</span>
                ) : (
                  "Not configured"
                )}
              </dd>
              <dt className="text-gray-500 dark:text-gray-400">Scopes</dt>
              <dd className="text-gray-800 dark:text-white">{formatList(provider.scopes)}</dd>
              <dt className="text-gray-500 dark:text-gray-400">Allowed Email Domains</dt>
              <dd className="text-gray-800 dark:text-white">{formatList(provider.allowedEmailDomains)}</dd>
              <dt className="text-gray-500 dark:text-gray-400">Allowed Groups</dt>
              <dd className="text-gray-800 dark:text-white">{formatList(provider.allowedGroups)}</dd>
              <dt className="text-gray-500 dark:text-gray-400">Group → Role mapping</dt>
              <dd className="text-gray-800 dark:text-white break-words">{formatMapping(provider.groupRoleMappings)}</dd>
              <dt className="text-gray-500 dark:text-gray-400">Created at</dt>
              <dd className="text-gray-800 dark:text-white">{createdAt}</dd>
              <dt className="text-gray-500 dark:text-gray-400">ID</dt>
              <dd className="text-gray-800 dark:text-white font-mono text-xs">{provider.id}</dd>
            </dl>
          )}
        </div>
      </div>

      {/* Callback URL */}
      <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-300 dark:border-gray-800 overflow-hidden">
        <div className="px-6 py-6">
          <h3 className="text-sm font-semibold text-gray-800 dark:text-white mb-2">Callback URL</h3>
          <p className="text-sm text-gray-500 dark:text-gray-400 mb-4">
            Register this redirect URI in your identity provider.
          </p>
          <div className="flex items-center gap-2">
            <Input
              readOnly
              value={callbackUrl}
              className="flex-1 font-mono text-xs bg-gray-50 dark:bg-gray-800"
              data-testid="sso-detail-callback-url"
            />
            <Button variant="outline" onClick={handleCopyCallbackUrl} data-testid="sso-detail-callback-copy">
              <Copy className="w-4 h-4" />
            </Button>
          </div>
        </div>
      </div>
    </div>
  );
}
