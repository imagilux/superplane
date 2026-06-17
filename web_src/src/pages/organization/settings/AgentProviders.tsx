import { Icon } from "@/components/Icon";
import { usePageTitle } from "@/hooks/usePageTitle";
import { PermissionTooltip } from "@/components/PermissionGate";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/Table/table";
import { Button } from "@/components/ui/button";
import { LoadingButton } from "@/components/ui/loading-button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import { Switch } from "@/ui/switch";
import { usePermissions } from "@/contexts/usePermissions";
import { getApiErrorMessage } from "@/lib/errors";
import { showErrorToast, showSuccessToast } from "@/lib/toast";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Sparkles } from "lucide-react";
import { useState } from "react";
import {
  useAgentProviders,
  useCreateAgentProvider,
  useUpdateAgentProvider,
  useDeleteAgentProvider,
} from "@/hooks/useAgentProviders";
import type { AgentProvidersAgentProvider } from "@/api-client/types.gen";

interface AgentProvidersProps {
  organizationId: string;
}

const SLUG_PATTERN = /^[a-z0-9-]+$/;
const DEFAULT_TYPE = "openai";

interface FormValues {
  displayName: string;
  slug: string;
  baseUrl: string;
  model: string;
}

const validateForm = (values: FormValues, isEditing: boolean): string | null => {
  if (!values.displayName.trim()) return "Display name is required";
  if (!isEditing) {
    if (!values.slug.trim()) return "Slug is required";
    if (!SLUG_PATTERN.test(values.slug.trim())) return "Slug may only contain lowercase letters, digits, and hyphens";
  }
  if (!values.baseUrl.trim()) return "Base URL is required";
  if (!values.model.trim()) return "Model is required";
  return null;
};

export function AgentProviders({ organizationId }: AgentProvidersProps) {
  usePageTitle(["Agent Providers"]);
  const { canAct, isLoading: permissionsLoading } = usePermissions();
  const [isModalOpen, setIsModalOpen] = useState(false);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [existingHasApiKey, setExistingHasApiKey] = useState(false);
  const [slug, setSlug] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [type, setType] = useState(DEFAULT_TYPE);
  const [baseUrl, setBaseUrl] = useState("");
  const [model, setModel] = useState("");
  const [apiKey, setApiKey] = useState("");
  const [enabled, setEnabled] = useState(true);

  const canCreate = canAct("agent_providers", "create");
  const canUpdate = canAct("agent_providers", "update");
  const canDelete = canAct("agent_providers", "delete");
  const isEditing = editingId !== null;

  const { data: providers = [], isLoading } = useAgentProviders(organizationId);
  const createMutation = useCreateAgentProvider(organizationId);
  const updateMutation = useUpdateAgentProvider(organizationId);
  const deleteMutation = useDeleteAgentProvider(organizationId);
  const saving = createMutation.isPending || updateMutation.isPending;

  const resetForm = () => {
    setSlug("");
    setDisplayName("");
    setType(DEFAULT_TYPE);
    setBaseUrl("");
    setModel("");
    setApiKey("");
    setEnabled(true);
    setExistingHasApiKey(false);
  };

  const handleCreateClick = () => {
    if (!canCreate) return;
    resetForm();
    setEditingId(null);
    setIsModalOpen(true);
  };

  const handleEditClick = (provider: AgentProvidersAgentProvider) => {
    setEditingId(provider.id || "");
    setExistingHasApiKey(!!provider.hasApiKey);
    setSlug(provider.slug || "");
    setDisplayName(provider.displayName || "");
    setType(provider.type || DEFAULT_TYPE);
    setBaseUrl(provider.baseUrl || "");
    setModel(provider.model || "");
    setApiKey("");
    setEnabled(!!provider.enabled);
    setIsModalOpen(true);
  };

  const handleCloseModal = () => {
    setIsModalOpen(false);
    setEditingId(null);
    resetForm();
    createMutation.reset();
    updateMutation.reset();
  };

  const handleSubmit = async () => {
    const validationError = validateForm({ displayName, slug, baseUrl, model }, isEditing);
    if (validationError) {
      showErrorToast(validationError);
      return;
    }
    try {
      if (isEditing && editingId) {
        if (!canUpdate) return;
        await updateMutation.mutateAsync({
          id: editingId,
          displayName: displayName.trim(),
          baseUrl: baseUrl.trim(),
          model: model.trim(),
          apiKey,
          enabled,
        });
        showSuccessToast("Provider updated");
      } else {
        if (!canCreate) return;
        await createMutation.mutateAsync({
          slug: slug.trim(),
          displayName: displayName.trim(),
          type,
          baseUrl: baseUrl.trim(),
          model: model.trim(),
          apiKey,
          enabled,
        });
        showSuccessToast("Provider created");
      }
      handleCloseModal();
    } catch (error) {
      showErrorToast(`Failed to save provider: ${getApiErrorMessage(error)}`);
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
              message="You don't have permission to create agent providers."
            >
              <Button
                className="flex items-center"
                onClick={handleCreateClick}
                disabled={!canCreate}
                data-testid="agent-provider-create-btn"
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
                <Sparkles size={32} />
              </div>
              <p className="mt-3 text-sm text-gray-800">Add your first agent provider</p>
              <p className="mt-1 text-xs text-gray-500">
                Point this organization's agents at a custom or local OpenAI-compatible endpoint.
              </p>
              <PermissionTooltip
                allowed={canCreate || permissionsLoading}
                message="You don't have permission to create agent providers."
              >
                <Button
                  className="mt-4 flex items-center"
                  onClick={handleCreateClick}
                  disabled={!canCreate}
                  data-testid="agent-provider-create-btn"
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
                  <TableHeader>Base URL</TableHeader>
                  <TableHeader>Model</TableHeader>
                  <TableHeader>Status</TableHeader>
                  <TableHeader></TableHeader>
                </TableRow>
              </TableHead>
              <TableBody>
                {sorted.map((provider) => (
                  <TableRow key={provider.id} className="last:[&>td]:border-b-0">
                    <TableCell>
                      <div className="flex items-center gap-2">
                        <Sparkles size={16} className="text-gray-500" />
                        {canUpdate ? (
                          <button
                            type="button"
                            onClick={() => handleEditClick(provider)}
                            className="cursor-pointer text-sm !font-semibold text-gray-800 dark:text-gray-100 !underline underline-offset-2"
                            data-testid="agent-provider-link"
                          >
                            {provider.displayName || "Unnamed"}
                          </button>
                        ) : (
                          <span className="text-sm font-semibold text-gray-800 dark:text-gray-100">
                            {provider.displayName || "Unnamed"}
                          </span>
                        )}
                      </div>
                    </TableCell>
                    <TableCell>
                      <span className="text-sm font-mono text-gray-500 dark:text-gray-400">{provider.slug || "—"}</span>
                    </TableCell>
                    <TableCell>
                      <span className="text-sm font-mono text-gray-500 dark:text-gray-400">
                        {provider.baseUrl || "—"}
                      </span>
                    </TableCell>
                    <TableCell>
                      <span className="text-sm text-gray-500 dark:text-gray-400">{provider.model || "—"}</span>
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
                          message="You don't have permission to delete agent providers."
                        >
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => handleDelete(provider.id || "", provider.displayName || "")}
                            disabled={!canDelete || deleteMutation.isPending}
                            className="text-red-600 hover:text-red-700"
                            data-testid="agent-provider-delete-btn"
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

      {/* Create / edit modal */}
      {isModalOpen && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50">
          <div className="bg-white dark:bg-gray-900 rounded-lg shadow-xl max-w-lg w-full mx-4 max-h-[90vh] overflow-y-auto">
            <form
              className="p-6"
              onSubmit={(e) => {
                e.preventDefault();
                handleSubmit();
              }}
              data-testid="agent-provider-form"
            >
              <div className="flex items-center justify-between mb-6">
                <div className="flex items-center gap-3">
                  <Sparkles className="w-6 h-6 text-gray-500 dark:text-gray-400" />
                  <h3 className="text-base font-semibold text-gray-800 dark:text-gray-100">
                    {isEditing ? "Edit agent provider" : "Add agent provider"}
                  </h3>
                </div>
                <button
                  type="button"
                  onClick={handleCloseModal}
                  className="text-gray-500 hover:text-gray-800 dark:hover:text-gray-300"
                  disabled={saving}
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
                    placeholder="e.g., Local llama.cpp"
                    required
                    data-testid="agent-provider-display-name"
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
                    placeholder="e.g., local-llama"
                    required
                    disabled={isEditing}
                    data-testid="agent-provider-slug"
                  />
                  <p className="mt-1 text-xs text-gray-500">
                    {isEditing
                      ? "The slug cannot be changed after creation."
                      : "Identifies the provider within this organization. Lowercase letters, digits, and hyphens only."}
                  </p>
                </div>
                <div>
                  <Label className="text-gray-800 dark:text-gray-100 mb-2">Type</Label>
                  <Select value={type} onValueChange={setType} disabled={isEditing}>
                    <SelectTrigger className="w-full" data-testid="agent-provider-type">
                      <SelectValue placeholder="Select a type" />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="openai">OpenAI-compatible</SelectItem>
                    </SelectContent>
                  </Select>
                  <p className="mt-1 text-xs text-gray-500">
                    An OpenAI-compatible <code>/chat/completions</code> endpoint — vLLM, Ollama, llama.cpp, OpenRouter,
                    and similar.
                  </p>
                </div>
                <div>
                  <Label className="text-gray-800 dark:text-gray-100 mb-2">
                    Base URL <span className="text-red-500">*</span>
                  </Label>
                  <Input
                    type="text"
                    value={baseUrl}
                    onChange={(e) => setBaseUrl(e.target.value)}
                    placeholder="e.g., http://localhost:8080/v1"
                    required
                    data-testid="agent-provider-base-url"
                  />
                  <p className="mt-1 text-xs text-gray-500">
                    The API base, including any version path. <code>/chat/completions</code> is appended automatically.
                  </p>
                </div>
                <div>
                  <Label className="text-gray-800 dark:text-gray-100 mb-2">
                    Model <span className="text-red-500">*</span>
                  </Label>
                  <Input
                    type="text"
                    value={model}
                    onChange={(e) => setModel(e.target.value)}
                    placeholder="e.g., qwen3"
                    required
                    data-testid="agent-provider-model"
                  />
                </div>
                <div>
                  <Label className="text-gray-800 dark:text-gray-100 mb-2">API Key</Label>
                  <Input
                    type="password"
                    value={apiKey}
                    onChange={(e) => setApiKey(e.target.value)}
                    placeholder={isEditing && existingHasApiKey ? "•••••••• (leave blank to keep)" : "Optional"}
                    className="ph-no-capture"
                    data-testid="agent-provider-api-key"
                  />
                  <p className="mt-1 text-xs text-gray-500">
                    Optional — unauthenticated local endpoints need none.
                    {isEditing && existingHasApiKey ? " Leave blank to keep the current key." : ""}
                  </p>
                </div>
                <div className="flex items-center gap-3">
                  <Switch checked={enabled} onCheckedChange={setEnabled} data-testid="agent-provider-enabled" />
                  <Label className="text-gray-800 dark:text-gray-100">Enabled</Label>
                </div>
                <p className="text-xs text-gray-500">
                  The enabled provider is used for this organization's agent sessions. If none is enabled, agents fall
                  back to the installation-wide provider.
                </p>
              </div>

              <div className="flex justify-start gap-3 mt-6">
                <PermissionTooltip
                  allowed={(isEditing ? canUpdate : canCreate) || permissionsLoading}
                  message={`You don't have permission to ${isEditing ? "update" : "create"} agent providers.`}
                >
                  <LoadingButton
                    type="submit"
                    disabled={(isEditing ? !canUpdate : !canCreate) || !displayName.trim()}
                    loading={saving}
                    loadingText={isEditing ? "Saving..." : "Creating..."}
                    className="flex items-center gap-2"
                    data-testid="agent-provider-submit"
                  >
                    {isEditing ? "Save changes" : "Create"}
                  </LoadingButton>
                </PermissionTooltip>
                <Button type="button" variant="outline" onClick={handleCloseModal} disabled={saving}>
                  Cancel
                </Button>
              </div>

              {(createMutation.isError || updateMutation.isError) && (
                <div className="mt-4 p-3 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md">
                  <p className="text-sm text-red-800 dark:text-red-200">
                    Failed to save: {getApiErrorMessage(createMutation.error || updateMutation.error)}
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
