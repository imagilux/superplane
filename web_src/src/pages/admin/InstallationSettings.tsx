import { Text } from "@/components/Text/text";
import { Input, InputGroup } from "@/components/Input/input";
import { Label } from "@/components/ui/label";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { showErrorToast, showSuccessToast } from "@/lib/toast";
import { Switch } from "@/ui/switch";
import React, { useCallback, useEffect, useState } from "react";

type PasswordOnlyAccount = {
  id: string;
  email: string;
  name: string;
};

type InstallationSettingsResponse = {
  allow_private_network_access: boolean;
  password_login_disabled: boolean;
  password_login_disable_allowed: boolean;
  password_login_disable_reason?: string;
  password_only_accounts: PasswordOnlyAccount[];
  effective_blocked_http_hosts: string[];
  effective_private_ip_ranges: string[];
  blocked_http_hosts_overridden: boolean;
  private_ip_ranges_overridden: boolean;
  smtp_enabled: boolean;
  smtp_host: string;
  smtp_port: number;
  smtp_username: string;
  smtp_from_name: string;
  smtp_from_email: string;
  smtp_use_tls: boolean;
  smtp_password_configured: boolean;
};

type SMTPFormState = {
  enabled: boolean;
  host: string;
  port: string;
  username: string;
  password: string;
  fromName: string;
  fromEmail: string;
  useTLS: boolean;
};

type DerivedState = {
  blockedHosts: string[];
  privateRanges: string[];
  hasNetworkChanges: boolean;
  hasIdentityChanges: boolean;
  hasSMTPChanges: boolean;
};

type NetworkPolicySectionProps = {
  allowPrivateNetworkAccess: boolean;
  blockedHosts: string[];
  blockedHTTPHostsOverridden: boolean;
  hasChanges: boolean;
  privateRanges: string[];
  privateIPRangesOverridden: boolean;
  saving: boolean;
  onChange: (checked: boolean) => void;
  onSave: () => void;
};

type IdentitySectionProps = {
  passwordLoginDisabled: boolean;
  passwordLoginDisableAllowed: boolean;
  passwordLoginDisableReason?: string;
  passwordOnlyAccounts: PasswordOnlyAccount[];
  disablingPasswordLogin: boolean;
  hasChanges: boolean;
  saving: boolean;
  onPasswordLoginChange: (checked: boolean) => void;
  onSave: (deactivateLockedOutAccounts: boolean) => void;
};

type SMTPSectionProps = {
  form: SMTPFormState;
  hasChanges: boolean;
  passwordConfigured: boolean;
  saving: boolean;
  onFieldChange: (field: keyof SMTPFormState, value: boolean | string) => void;
  onSave: () => void;
};

type SMTPFieldsProps = {
  form: SMTPFormState;
  passwordConfigured: boolean;
  onFieldChange: (field: keyof SMTPFormState, value: boolean | string) => void;
};

const emptySMTPForm: SMTPFormState = {
  enabled: false,
  host: "",
  port: "",
  username: "",
  password: "",
  fromName: "",
  fromEmail: "",
  useTLS: true,
};

const getErrorMessage = async (response: Response, fallback: string) => {
  const text = await response.text();
  if (text.trim() === "") {
    return fallback;
  }

  return text;
};

const normalizeSMTPPort = (port: string) => {
  if (port.trim() === "") {
    return 0;
  }

  return Number(port.trim());
};

const toSMTPFormState = (data: InstallationSettingsResponse): SMTPFormState => ({
  enabled: data.smtp_enabled,
  host: data.smtp_host,
  port: data.smtp_port > 0 ? String(data.smtp_port) : "",
  username: data.smtp_username,
  password: "",
  fromName: data.smtp_from_name,
  fromEmail: data.smtp_from_email,
  useTLS: data.smtp_enabled ? data.smtp_use_tls : true,
});

const getDerivedState = (
  settings: InstallationSettingsResponse | null,
  allowPrivateNetworkAccess: boolean,
  passwordLoginDisabled: boolean,
  form: SMTPFormState,
): DerivedState => {
  const hasSMTPSettings = settings?.smtp_enabled ?? false;

  return {
    blockedHosts: settings?.effective_blocked_http_hosts ?? [],
    privateRanges: settings?.effective_private_ip_ranges ?? [],
    hasNetworkChanges: settings != null && allowPrivateNetworkAccess !== settings.allow_private_network_access,
    hasIdentityChanges: settings != null && passwordLoginDisabled !== settings.password_login_disabled,
    hasSMTPChanges:
      settings != null &&
      (form.enabled !== settings.smtp_enabled ||
        form.host.trim() !== settings.smtp_host ||
        normalizeSMTPPort(form.port) !== settings.smtp_port ||
        form.username.trim() !== settings.smtp_username ||
        form.fromName.trim() !== settings.smtp_from_name ||
        form.fromEmail.trim() !== settings.smtp_from_email ||
        ((form.enabled || hasSMTPSettings) && form.useTLS !== settings.smtp_use_tls) ||
        form.password !== ""),
  };
};

const buildSMTPRequestBody = (form: SMTPFormState) => {
  const body: Record<string, unknown> = {
    smtp_enabled: form.enabled,
  };

  if (!form.enabled) {
    return body;
  }

  body.smtp_host = form.host.trim();
  body.smtp_port = normalizeSMTPPort(form.port);
  body.smtp_username = form.username.trim();
  body.smtp_from_name = form.fromName.trim();
  body.smtp_from_email = form.fromEmail.trim();
  body.smtp_use_tls = form.useTLS;

  if (form.password !== "") {
    body.smtp_password = form.password;
  }

  return body;
};

const NetworkPolicySection = ({
  allowPrivateNetworkAccess,
  blockedHosts,
  blockedHTTPHostsOverridden,
  hasChanges,
  privateRanges,
  privateIPRangesOverridden,
  saving,
  onChange,
  onSave,
}: NetworkPolicySectionProps) => (
  <div className="rounded-xl border border-slate-200 bg-gradient-to-br from-white to-slate-50 p-6 shadow-sm">
    <div className="flex flex-col gap-6 lg:flex-row lg:items-start lg:justify-between">
      <div className="max-w-2xl">
        <div className="inline-flex rounded-full bg-slate-900 px-3 py-1 text-[11px] font-semibold uppercase tracking-[0.18em] text-white">
          Network Policy
        </div>
        <h2 className="mt-4 text-lg font-semibold text-gray-900">Private network access</h2>
        <Text className="mt-2 text-sm text-gray-600">
          Control whether integrations, components, and triggers can reach internal Kubernetes DNS names or private IP
          ranges.
        </Text>
      </div>

      <div
        className={`rounded-2xl border px-4 py-3 shadow-sm ${
          allowPrivateNetworkAccess ? "border-emerald-200 bg-emerald-50" : "border-amber-200 bg-amber-50"
        }`}
      >
        <div className="flex items-center gap-4">
          <div
            className={`flex h-11 w-11 items-center justify-center rounded-full ${
              allowPrivateNetworkAccess ? "bg-emerald-500/15" : "bg-amber-500/15"
            }`}
          >
            <span className={`h-3 w-3 rounded-full ${allowPrivateNetworkAccess ? "bg-emerald-600" : "bg-amber-600"}`} />
          </div>

          <div>
            <p className="text-sm font-semibold text-gray-900">
              {allowPrivateNetworkAccess ? "Enabled for private targets" : "Blocked for private targets"}
            </p>
            <Text className="mt-1 text-xs text-gray-600">
              {allowPrivateNetworkAccess
                ? "SuperPlane can reach internal hosts and private IPs."
                : "SSRF safeguards remain active for internal hosts and private IPs."}
            </Text>
          </div>

          <Switch
            data-testid="installation-network-switch"
            checked={allowPrivateNetworkAccess}
            onCheckedChange={onChange}
          />
        </div>
      </div>
    </div>

    <div className="mt-6">
      <div className="flex flex-col gap-6 lg:flex-row">
        <div className="min-w-0 flex-1">
          <h2 className="text-base font-medium text-gray-900">Effective blocked hosts</h2>
          <Text className="mt-1 text-sm text-gray-500">
            {blockedHTTPHostsOverridden ? "Overridden by env var." : "Resolved from installation settings."}
          </Text>
          <div className="mt-4 rounded-xl border border-slate-200/80 p-4 bg-slate-100">
            {blockedHosts.length === 0 ? (
              <Text className="text-sm text-gray-500">No blocked hosts.</Text>
            ) : (
              <ul className="space-y-1 font-mono text-xs text-gray-700">
                {blockedHosts.map((host) => (
                  <li key={host}>{host}</li>
                ))}
              </ul>
            )}
          </div>
        </div>

        <div className="min-w-0 flex-1">
          <h2 className="text-base font-medium text-gray-900">Effective blocked private IP ranges</h2>
          <Text className="mt-1 text-sm text-gray-500">
            {privateIPRangesOverridden ? "Overridden by env var." : "Resolved from installation settings."}
          </Text>
          <div className="mt-4 rounded-xl border border-slate-200/80 p-4 bg-slate-100">
            {privateRanges.length === 0 ? (
              <Text className="text-sm text-gray-500">No blocked private IP ranges.</Text>
            ) : (
              <ul className="space-y-1 font-mono text-xs text-gray-700">
                {privateRanges.map((range) => (
                  <li key={range}>{range}</li>
                ))}
              </ul>
            )}
          </div>
        </div>
      </div>
    </div>

    <div className="mt-6 flex flex-wrap items-center gap-3 border-t border-slate-200 pt-6">
      <Button type="button" data-testid="installation-network-save" onClick={onSave} disabled={saving || !hasChanges}>
        {saving ? "Saving..." : "Save network settings"}
      </Button>
      {blockedHTTPHostsOverridden || privateIPRangesOverridden ? (
        <Text className="text-xs text-amber-700">
          Environment overrides are active. `BLOCKED_HTTP_HOSTS` and `BLOCKED_PRIVATE_IP_RANGES` take precedence over
          this toggle.
        </Text>
      ) : (
        <Text className="text-xs text-gray-500">
          Changes apply without a restart and may take a few seconds to propagate across all app instances.
        </Text>
      )}
    </div>
  </div>
);

const IdentitySection = ({
  passwordLoginDisabled,
  passwordLoginDisableAllowed,
  passwordLoginDisableReason,
  passwordOnlyAccounts,
  disablingPasswordLogin,
  hasChanges,
  saving,
  onPasswordLoginChange,
  onSave,
}: IdentitySectionProps) => {
  const [confirmOpen, setConfirmOpen] = useState(false);

  // Re-enabling password login is always allowed; only disabling can lock the admin out.
  const toggleDisabled = !passwordLoginDisabled && !passwordLoginDisableAllowed;
  const showDisableWarning = toggleDisabled && passwordLoginDisableReason != null && passwordLoginDisableReason !== "";

  // A real transition from "enabled" to "disabled" that would strand password-only accounts
  // requires explicit confirmation before deactivating them. Re-enabling, or disabling when
  // nobody is stranded, saves directly.
  const needsConfirmation = disablingPasswordLogin && passwordOnlyAccounts.length > 0;

  const handleSave = () => {
    if (needsConfirmation) {
      setConfirmOpen(true);
      return;
    }

    onSave(false);
  };

  const handleConfirmDeactivate = () => {
    onSave(true);
    setConfirmOpen(false);
  };

  return (
    <div className="rounded-xl border border-slate-200 bg-gradient-to-br from-white to-slate-50 p-6 shadow-sm">
      <div className="flex flex-col gap-6 lg:flex-row lg:items-start lg:justify-between">
        <div className="max-w-2xl">
          <div className="inline-flex rounded-full bg-slate-900 px-3 py-1 text-[11px] font-semibold uppercase tracking-[0.18em] text-white">
            Identity
          </div>
          <h2 className="mt-4 text-lg font-semibold text-gray-900">Login methods</h2>
          <Text className="mt-2 text-sm text-gray-600">
            Control which authentication methods are available to users when signing in to this installation.
          </Text>
        </div>
      </div>

      <div className="mt-6 rounded-2xl border border-slate-200 bg-slate-50 px-4 py-3 shadow-sm">
        <div className="flex items-center justify-between gap-4">
          <div>
            <p className="text-sm font-semibold text-gray-900">Disable email/password login</p>
            <Text className="mt-1 text-xs text-gray-600">
              Hide the email/password form and reject password logins for the whole installation. SSO logins and
              existing sessions are unaffected. This is independent of signup blocking (BLOCK_SIGNUP).
            </Text>
          </div>
          <Switch
            data-testid="installation-password-login-switch"
            checked={passwordLoginDisabled}
            disabled={toggleDisabled}
            onCheckedChange={onPasswordLoginChange}
          />
        </div>
        {showDisableWarning ? (
          <Text data-testid="installation-password-login-warning" className="mt-3 text-xs text-amber-700">
            {passwordLoginDisableReason}
          </Text>
        ) : null}
      </div>

      <div className="mt-6 flex flex-wrap items-center gap-3 border-t border-slate-200 pt-6">
        <Button
          type="button"
          data-testid="installation-identity-save"
          onClick={handleSave}
          disabled={saving || !hasChanges}
        >
          {saving ? "Saving..." : "Save identity settings"}
        </Button>
        <Text className="text-xs text-gray-500">
          Changes apply without a restart and may take a few seconds to propagate across all app instances.
        </Text>
      </div>

      <Dialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <DialogContent data-testid="installation-password-login-confirm-dialog">
          <DialogHeader>
            <DialogTitle>Disable password login?</DialogTitle>
            <DialogDescription asChild>
              <div className="space-y-3">
                <p className="text-sm text-gray-600">
                  The following {passwordOnlyAccounts.length === 1 ? "account" : "accounts"} can sign in only with a
                  password and would be locked out. Disabling password login will deactivate{" "}
                  {passwordOnlyAccounts.length === 1 ? "it" : "them"} (not delete{" "}
                  {passwordOnlyAccounts.length === 1 ? "it" : "them"}); you can reactivate{" "}
                  {passwordOnlyAccounts.length === 1 ? "it" : "them"} later.
                </p>
                <ul
                  data-testid="installation-password-login-confirm-list"
                  className="max-h-60 space-y-1 overflow-y-auto rounded-xl border border-slate-200 bg-slate-50 p-4 text-sm text-gray-700"
                >
                  {passwordOnlyAccounts.map((account) => (
                    <li key={account.id}>
                      <span className="font-medium text-gray-900">{account.name}</span>{" "}
                      <span className="text-gray-500">{account.email}</span>
                    </li>
                  ))}
                </ul>
              </div>
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              data-testid="installation-password-login-confirm-cancel"
              onClick={() => setConfirmOpen(false)}
              disabled={saving}
            >
              Cancel
            </Button>
            <Button
              type="button"
              data-testid="installation-password-login-confirm-deactivate"
              onClick={handleConfirmDeactivate}
              disabled={saving}
            >
              {saving ? "Saving..." : "Deactivate & disable"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
};

const SMTPFields = ({ form, passwordConfigured, onFieldChange }: SMTPFieldsProps) => (
  <div className="mt-6 space-y-4">
    <div className="grid gap-4 md:grid-cols-2">
      <div>
        <Label className="mb-2 block text-left">SMTP Host</Label>
        <InputGroup>
          <Input
            value={form.host}
            onChange={(event) => onFieldChange("host", event.target.value)}
            placeholder="smtp.example.com"
          />
        </InputGroup>
      </div>

      <div>
        <Label className="mb-2 block text-left">SMTP Port</Label>
        <InputGroup>
          <Input value={form.port} onChange={(event) => onFieldChange("port", event.target.value)} placeholder="587" />
        </InputGroup>
      </div>

      <div>
        <Label className="mb-2 block text-left">SMTP Username</Label>
        <InputGroup>
          <Input
            value={form.username}
            onChange={(event) => onFieldChange("username", event.target.value)}
            placeholder="smtp-user"
          />
        </InputGroup>
      </div>

      <div>
        <Label className="mb-2 block text-left">SMTP Password</Label>
        <InputGroup>
          <Input
            type="password"
            data-testid="installation-smtp-password"
            value={form.password}
            onChange={(event) => onFieldChange("password", event.target.value)}
            placeholder={passwordConfigured ? "Leave blank to keep current password" : "SMTP password"}
            className="ph-no-capture"
          />
        </InputGroup>
        {passwordConfigured ? (
          <Text className="mt-1 text-xs text-gray-500">Leave blank to keep the existing SMTP password.</Text>
        ) : null}
      </div>

      <div>
        <Label className="mb-2 block text-left">From Name</Label>
        <InputGroup>
          <Input
            value={form.fromName}
            onChange={(event) => onFieldChange("fromName", event.target.value)}
            placeholder="SuperPlane"
          />
        </InputGroup>
      </div>

      <div>
        <Label className="mb-2 block text-left">From Email</Label>
        <InputGroup>
          <Input
            type="email"
            value={form.fromEmail}
            onChange={(event) => onFieldChange("fromEmail", event.target.value)}
            placeholder="noreply@example.com"
          />
        </InputGroup>
      </div>
    </div>

    <div className="rounded-xl border border-slate-200 bg-slate-50 px-4 py-3">
      <div className="flex items-center justify-between gap-4">
        <div>
          <p className="text-sm font-medium text-gray-900">Use TLS (STARTTLS)</p>
          <Text className="mt-1 text-xs text-gray-600">
            Enable transport encryption when connecting to the SMTP server.
          </Text>
        </div>
        <Switch
          data-testid="installation-smtp-tls-switch"
          checked={form.useTLS}
          onCheckedChange={(checked) => onFieldChange("useTLS", checked)}
        />
      </div>
    </div>
  </div>
);

const SMTPSection = ({ form, hasChanges, passwordConfigured, saving, onFieldChange, onSave }: SMTPSectionProps) => (
  <div className="rounded-xl border border-slate-200 bg-white p-6 shadow-sm">
    <div className="flex flex-col gap-6 lg:flex-row lg:items-start lg:justify-between">
      <div className="max-w-2xl">
        <div className="inline-flex rounded-full bg-slate-100 px-3 py-1 text-[11px] font-semibold uppercase tracking-[0.18em] text-slate-700">
          Email Delivery
        </div>
        <h2 className="mt-4 text-lg font-semibold text-gray-900">SMTP configuration</h2>
        <Text className="mt-2 text-sm text-gray-600">
          Manage the SMTP credentials used for installation-wide notifications and emails.
        </Text>
      </div>

      <div className="rounded-2xl border border-slate-200 bg-slate-50 px-4 py-3 shadow-sm">
        <div className="flex items-center gap-4">
          <div className="space-y-1">
            <p className="text-sm font-semibold text-gray-900">{form.enabled ? "SMTP enabled" : "SMTP disabled"}</p>
            <Text className="text-xs text-gray-600">
              {form.enabled ? "Emails can be sent from this instance." : "Notification email delivery is turned off."}
            </Text>
          </div>

          <Switch
            data-testid="installation-smtp-switch"
            checked={form.enabled}
            onCheckedChange={(checked) => onFieldChange("enabled", checked)}
          />
        </div>
      </div>
    </div>

    {form.enabled ? (
      <SMTPFields form={form} passwordConfigured={passwordConfigured} onFieldChange={onFieldChange} />
    ) : (
      <div className="mt-6 rounded-xl border border-dashed border-slate-300 bg-slate-50 p-6">
        <Text className="text-sm text-gray-600">
          Enable SMTP to configure the installation-wide email provider used by SuperPlane notifications.
        </Text>
      </div>
    )}

    <div className="mt-6 flex items-center gap-3">
      <Button type="button" data-testid="installation-smtp-save" onClick={onSave} disabled={saving || !hasChanges}>
        {saving ? "Saving..." : form.enabled ? "Save SMTP settings" : "Disable SMTP"}
      </Button>
      <Text className="text-xs text-gray-500">SMTP changes apply to installation-wide email delivery.</Text>
    </div>
  </div>
);

const useInstallationSettingsState = () => {
  const [settings, setSettings] = useState<InstallationSettingsResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [allowPrivateNetworkAccess, setAllowPrivateNetworkAccess] = useState(false);
  const [passwordLoginDisabled, setPasswordLoginDisabled] = useState(false);
  const [savingNetwork, setSavingNetwork] = useState(false);
  const [savingIdentity, setSavingIdentity] = useState(false);
  const [smtpForm, setSMTPForm] = useState<SMTPFormState>(emptySMTPForm);
  const [savingSMTP, setSavingSMTP] = useState(false);

  const applySettings = useCallback((data: InstallationSettingsResponse) => {
    setSettings(data);
    setAllowPrivateNetworkAccess(data.allow_private_network_access);
    setPasswordLoginDisabled(data.password_login_disabled);
    setSMTPForm(toSMTPFormState(data));
  }, []);

  const loadSettings = useCallback(async () => {
    setLoading(true);
    try {
      const response = await fetch("/admin/api/installation/network-settings", { credentials: "include" });
      if (!response.ok) {
        throw new Error(await getErrorMessage(response, "Failed to load installation settings"));
      }

      applySettings(await response.json());
    } catch (error) {
      showErrorToast(error instanceof Error ? error.message : "Failed to load installation settings");
    } finally {
      setLoading(false);
    }
  }, [applySettings]);

  useEffect(() => {
    loadSettings();
  }, [loadSettings]);

  const patchSettings = useCallback(
    async (body: Record<string, unknown>, successMessage: string, fallbackError: string) => {
      const response = await fetch("/admin/api/installation/network-settings", {
        method: "PATCH",
        headers: {
          "Content-Type": "application/json",
        },
        credentials: "include",
        body: JSON.stringify(body),
      });

      if (!response.ok) {
        throw new Error(await getErrorMessage(response, fallbackError));
      }

      applySettings(await response.json());
      showSuccessToast(successMessage);
    },
    [applySettings],
  );

  const saveNetworkSettings = useCallback(async () => {
    setSavingNetwork(true);
    try {
      await patchSettings(
        {
          allow_private_network_access: allowPrivateNetworkAccess,
        },
        "Network settings updated",
        "Failed to update installation settings",
      );
    } catch (error) {
      showErrorToast(error instanceof Error ? error.message : "Failed to update installation settings");
    } finally {
      setSavingNetwork(false);
    }
  }, [allowPrivateNetworkAccess, patchSettings]);

  const saveIdentitySettings = useCallback(
    async (deactivateLockedOutAccounts: boolean) => {
      setSavingIdentity(true);
      try {
        const body: Record<string, unknown> = {
          password_login_disabled: passwordLoginDisabled,
        };

        if (deactivateLockedOutAccounts) {
          body.deactivate_locked_out_accounts = true;
        }

        await patchSettings(body, "Identity settings updated", "Failed to update identity settings");
      } catch (error) {
        showErrorToast(error instanceof Error ? error.message : "Failed to update identity settings");
      } finally {
        setSavingIdentity(false);
      }
    },
    [passwordLoginDisabled, patchSettings],
  );

  const saveSMTPSettings = useCallback(async () => {
    setSavingSMTP(true);
    try {
      await patchSettings(buildSMTPRequestBody(smtpForm), "SMTP settings updated", "Failed to update SMTP settings");
    } catch (error) {
      showErrorToast(error instanceof Error ? error.message : "Failed to update SMTP settings");
    } finally {
      setSavingSMTP(false);
    }
  }, [patchSettings, smtpForm]);

  const setSMTPField = useCallback((field: keyof SMTPFormState, value: boolean | string) => {
    setSMTPForm((current) => ({
      ...current,
      [field]: value,
    }));
  }, []);

  return {
    settings,
    loading,
    allowPrivateNetworkAccess,
    passwordLoginDisabled,
    savingNetwork,
    savingIdentity,
    smtpForm,
    savingSMTP,
    setAllowPrivateNetworkAccess,
    setPasswordLoginDisabled,
    setSMTPField,
    saveNetworkSettings,
    saveIdentitySettings,
    saveSMTPSettings,
  };
};

const InstallationSettings: React.FC = () => {
  const {
    settings,
    loading,
    allowPrivateNetworkAccess,
    passwordLoginDisabled,
    savingNetwork,
    savingIdentity,
    smtpForm,
    savingSMTP,
    setAllowPrivateNetworkAccess,
    setPasswordLoginDisabled,
    setSMTPField,
    saveNetworkSettings,
    saveIdentitySettings,
    saveSMTPSettings,
  } = useInstallationSettingsState();

  if (loading && !settings) {
    return (
      <div className="flex flex-col items-center space-y-4 py-12">
        <div className="h-8 w-8 animate-spin rounded-full border-b border-gray-500"></div>
        <Text className="text-gray-500">Loading installation settings...</Text>
      </div>
    );
  }

  const derivedState = getDerivedState(settings, allowPrivateNetworkAccess, passwordLoginDisabled, smtpForm);

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-xl font-semibold text-gray-900">Installation Settings</h1>
        <Text className="mt-1 text-sm text-gray-500">
          Configure installation-wide network policy and email delivery for this SuperPlane instance.
        </Text>
      </div>

      <NetworkPolicySection
        allowPrivateNetworkAccess={allowPrivateNetworkAccess}
        blockedHosts={derivedState.blockedHosts}
        blockedHTTPHostsOverridden={settings?.blocked_http_hosts_overridden ?? false}
        hasChanges={derivedState.hasNetworkChanges}
        privateRanges={derivedState.privateRanges}
        privateIPRangesOverridden={settings?.private_ip_ranges_overridden ?? false}
        saving={savingNetwork}
        onChange={setAllowPrivateNetworkAccess}
        onSave={saveNetworkSettings}
      />

      <IdentitySection
        passwordLoginDisabled={passwordLoginDisabled}
        passwordLoginDisableAllowed={settings?.password_login_disable_allowed ?? true}
        passwordLoginDisableReason={settings?.password_login_disable_reason}
        passwordOnlyAccounts={settings?.password_only_accounts ?? []}
        disablingPasswordLogin={passwordLoginDisabled && !(settings?.password_login_disabled ?? false)}
        hasChanges={derivedState.hasIdentityChanges}
        saving={savingIdentity}
        onPasswordLoginChange={setPasswordLoginDisabled}
        onSave={saveIdentitySettings}
      />

      <SMTPSection
        form={smtpForm}
        hasChanges={derivedState.hasSMTPChanges}
        passwordConfigured={settings?.smtp_password_configured ?? false}
        saving={savingSMTP}
        onFieldChange={setSMTPField}
        onSave={saveSMTPSettings}
      />
    </div>
  );
};

export default InstallationSettings;
