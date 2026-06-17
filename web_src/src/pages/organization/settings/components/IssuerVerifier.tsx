import { useEffect, useState } from "react";
import { Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useDiscoverOidcProvider } from "@/hooks/useOidcProviders";
import { getApiErrorMessage } from "@/lib/errors";
import type { OidcProvidersDiscoverOidcProviderResponse } from "@/api-client/types.gen";

const RELEVANT_SCOPES = ["openid", "email", "profile"] as const;

export interface IssuerVerifierProps {
  issuerUrl: string;
  organizationId: string;
  /** Called with the relevant supported scopes (subset of openid/email/profile) when discovery succeeds. */
  onScopesDiscovered: (scopes: string) => void;
}

const relevantSupportedScopes = (scopesSupported: string[] | undefined): string[] => {
  if (!scopesSupported || scopesSupported.length === 0) return [];
  const supported = new Set(scopesSupported);
  return RELEVANT_SCOPES.filter((scope) => supported.has(scope));
};

function VerifyResult({ result }: { result: OidcProvidersDiscoverOidcProviderResponse }) {
  if (result.valid === false) {
    return (
      <p className="mt-2 text-xs text-red-600 dark:text-red-400" data-testid="sso-verify-error">
        ✗ Could not verify issuer: {result.error || "unknown error"}
      </p>
    );
  }

  return (
    <div className="mt-2 space-y-1">
      <p className="text-xs text-green-600 dark:text-green-400" data-testid="sso-verify-success">
        ✓ Valid OIDC issuer{result.issuer ? ` (${result.issuer})` : ""}
      </p>
      {result.emailVerifiedSupported === false && (
        <p className="text-xs text-amber-600 dark:text-amber-400" data-testid="sso-verify-warning">
          ⚠ This provider doesn't advertise an <code>email_verified</code> claim — SSO logins may be rejected by the
          verified-email check.
        </p>
      )}
    </div>
  );
}

export function IssuerVerifier({ issuerUrl, organizationId, onScopesDiscovered }: IssuerVerifierProps) {
  const discoverMutation = useDiscoverOidcProvider(organizationId);
  const [result, setResult] = useState<OidcProvidersDiscoverOidcProviderResponse | null>(null);

  // Reset the indicator whenever the issuer URL changes (clearing or editing it).
  useEffect(() => {
    setResult(null);
  }, [issuerUrl]);

  const handleVerify = async () => {
    const trimmed = issuerUrl.trim();
    if (!trimmed) return;
    try {
      const data = await discoverMutation.mutateAsync(trimmed);
      setResult(data);
      if (data.valid) {
        const scopes = relevantSupportedScopes(data.scopesSupported);
        if (scopes.length > 0) onScopesDiscovered(scopes.join(", "));
      }
    } catch (error) {
      setResult({ valid: false, error: getApiErrorMessage(error) });
    }
  };

  return (
    <div className="mt-2">
      <Button
        type="button"
        variant="outline"
        size="sm"
        onClick={handleVerify}
        disabled={!issuerUrl.trim() || discoverMutation.isPending}
        data-testid="sso-verify-btn"
      >
        {discoverMutation.isPending ? (
          <>
            <Loader2 className="h-4 w-4 animate-spin" />
            Verifying…
          </>
        ) : (
          "Verify issuer"
        )}
      </Button>
      {result && <VerifyResult result={result} />}
    </div>
  );
}
