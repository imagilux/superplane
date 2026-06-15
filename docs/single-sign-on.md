# Single Sign-On (per-organization OIDC)

SuperPlane organizations can authenticate members through their own generic
**OpenID Connect (OIDC)** identity provider — Authelia, Keycloak, Okta, Microsoft
Entra ID, Google, or any spec-compliant IdP that exposes an OIDC discovery
document. SSO is configured per organization and runs alongside the built-in
GitHub/Google login and email/password options.

> SAML 2.0 is not yet supported. The provider type selector reserves it, but
> creating a SAML provider is rejected.

## How it works

1. An org admin registers one or more OIDC providers for the organization.
2. A user goes to the login page, chooses **Sign in with SSO**, and enters their
   work email. SuperPlane looks up which organization provider serves that email
   domain and redirects to the matching IdP (authorization-code flow).
3. On return, SuperPlane verifies the ID token, **requires a verified email**,
   checks the email domain against the provider's allow-list, links the identity
   to a SuperPlane account, and — if the user is not yet a member — provisions
   them into the organization with the **Viewer** role (just-in-time).

Security properties: the client secret is encrypted at rest; the flow uses a
signed, short-lived `state` + `nonce` cookie for CSRF and replay protection; only
ID tokens with `email_verified: true` are accepted; and an account is only
provisioned when the email domain is allow-listed (or signup is otherwise
permitted).

## Prerequisites

- The **Admin** (or Owner) role in the SuperPlane organization.
- An OIDC identity provider where you can register a confidential client and
  obtain a **client ID** and **client secret**.
- Your SuperPlane base URL (e.g. `https://superplane.example.com`).

## Step 1 — Register a client in your IdP

Create an OAuth2/OIDC **confidential client** (authorization code flow) in your
IdP with this **redirect URI**:

```
https://<your-superplane-base-url>/auth/sso/<organization-id>/<provider-slug>/callback
```

The exact URL is shown (read-only, copyable) on the provider form in SuperPlane
once you choose a slug, so the easiest path is to start Step 2, copy the callback
URL, then finish registering the client. Request at least the `openid`, `email`,
and `profile` scopes and ensure the IdP issues a verified `email` claim.

## Step 2 — Add the provider in SuperPlane

Go to **Organization Settings → Single Sign-On → Add provider** and fill in:

| Field | Notes |
|---|---|
| Display name | Shown on the login button, e.g. "Acme SSO". |
| Slug | Lowercase letters, digits, and hyphens. Used in the login/callback URL; immutable after creation. |
| Issuer URL | The IdP's issuer (discovery base). SuperPlane fetches `<issuer>/.well-known/openid-configuration`. |
| Client ID / Client secret | From Step 1. The secret is write-only and never displayed again. |
| Scopes | Defaults to `openid, email, profile`. |
| Allowed email domains | Emails whose domain is listed may use this provider, and the provider is offered for those domains on the login page. Leave empty to impose no restriction (reachable only via a direct login URL). |
| Enabled | Toggle the provider on/off without deleting it. |

### Issuer URL by provider

| IdP | Issuer URL |
|---|---|
| Authelia | `https://auth.example.com` |
| Keycloak | `https://keycloak.example.com/realms/<realm>` |
| Okta | `https://<tenant>.okta.com` (or your custom auth-server issuer) |
| Microsoft Entra ID | `https://login.microsoftonline.com/<tenant-id>/v2.0` |
| Google | `https://accounts.google.com` |

## Step 3 — Test the login

1. Log out, then open the login page and choose **Sign in with SSO**.
2. Enter an email whose domain you allow-listed. You are redirected to the IdP.
3. After authenticating, you return to SuperPlane: a session is established, and
   (on first login) you are added to the organization as a **Viewer**. An admin
   can then elevate your role.

## Configuring without the UI

Everything above is also available over the REST/gRPC API, which is convenient
for automation or air-gapped provisioning:

```
POST /api/v1/oidc-providers
{
  "slug": "acme",
  "displayName": "Acme SSO",
  "issuerUrl": "https://auth.example.com",
  "clientId": "superplane",
  "clientSecret": "…",
  "scopes": ["openid", "email", "profile"],
  "allowedEmailDomains": ["example.com"],
  "enabled": true
}
```

`GET/PATCH/DELETE /api/v1/oidc-providers[/{id}]` manage existing providers. The
client secret is write-only: responses report `hasClientSecret` but never the
value, and an empty `clientSecret` on update keeps the current one.

## Notes and limitations

- **Multiple providers per organization** are supported (one row each); a user
  reaches a specific one by email-domain discovery or a direct login URL.
- **Same email across organizations**: one account, one membership per org. The
  provider identity is namespaced per provider, so the same IdP subject can map
  cleanly across orgs.
- **Discovery caching**: OIDC discovery is cached (default 10 minutes, override
  with the `SSO_DISCOVERY_TTL` environment variable). Issuer/secret changes
  propagate within the TTL; in multi-instance deployments, invalidation is per
  instance and converges via the TTL.
- **Verified email required**: ID tokens without `email_verified: true` are
  rejected. Ensure your IdP issues verified emails for SSO users.
- **SAML 2.0** is not yet supported.
