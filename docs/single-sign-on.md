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
   checks the email domain (and, if configured, the IdP **group** allow-list),
   links the identity to a SuperPlane account, and provisions the user into the
   organization just-in-time. The granted role is **Viewer** by default — or, if
   the provider has a **group → role mapping**, the role computed from the user's
   IdP groups. With a mapping configured the role is **re-synced on every login**
   (the IdP is the source of truth), so a manually changed role is reverted on
   the next sign-in; an organization **Owner is never demoted** this way.

Security properties: the client secret is encrypted at rest (AES-GCM, bound to
the provider row); the flow uses a signed, short-lived `state` + `nonce` cookie
for CSRF and replay protection; only ID tokens with `email_verified: true` are
accepted; and an account is only provisioned when the email domain (and the group
allow-list, if set) permits it. All server-side fetches to the IdP (discovery and
JWKS) pass through an **SSRF guard** that blocks loopback, link-local, and cloud
metadata endpoints — while intentionally allowing private/RFC-1918 addresses so a
self-hosted IdP on an internal network works.

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
and `profile` scopes and ensure the IdP issues a verified `email` claim. If you
use group gating or a group→role mapping, also configure the IdP to emit a
`groups` claim (SuperPlane requests the `groups` scope automatically when either
is set).

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
| Allowed groups | If set, only users in at least one of these IdP groups may sign in; everyone else is rejected (`group_not_allowed`). Leave empty for no group restriction. Requires the IdP to emit a `groups` claim. |
| Group → role mapping | Maps an IdP group name to a SuperPlane org role — **Admin** or **Viewer** (Owner is never group-assigned). Highest-precedence match wins; unmapped users get Viewer. When set, roles are re-synced on every login (see [How it works](#how-it-works)). |
| Enabled | Toggle the provider on/off without deleting it. |

**Tip — auto-fill from discovery.** After entering the Issuer URL, click **Verify
issuer**. SuperPlane fetches the IdP's `/.well-known/openid-configuration`,
confirms it's reachable through the SSRF guard, and pre-fills the scopes/claims it
advertises (warning you if it doesn't advertise `email_verified`). This is the
same probe exposed at `POST /api/v1/oidc-providers/discover`.

### Issuer URL by provider

| IdP | Issuer URL |
|---|---|
| Authelia | `https://auth.example.com` |
| Keycloak | `https://keycloak.example.com/realms/<realm>` |
| Okta | `https://<tenant>.okta.com` (or your custom auth-server issuer) |
| Microsoft Entra ID | `https://login.microsoftonline.com/<tenant-id>/v2.0` |
| Google | `https://accounts.google.com` |

> **Group claim naming.** SuperPlane reads group membership from the `groups`
> claim of the ID token. IdPs differ here — Authelia and Keycloak emit `groups`,
> while Okta and Entra ID may use a different claim name or return group object
> IDs. If your IdP doesn't put group names in a `groups` claim, group gating and
> role mapping won't see them. (A configurable claim name is on the roadmap.)

## Step 3 — Test the login

1. Log out, then open the login page and choose **Sign in with SSO**.
2. Enter an email whose domain you allow-listed. You are redirected to the IdP.
3. After authenticating, you return to SuperPlane: a session is established, and
   (on first login) you are added to the organization — as a **Viewer**, or with
   the role from the provider's group→role mapping if one applies. With a mapping
   configured, the role is re-synced on each login rather than set once.

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
  "allowedGroups": ["superplane-users"],
  "groupRoleMappings": { "superplane-admins": "org_admin" },
  "enabled": true
}
```

`GET/PATCH/DELETE /api/v1/oidc-providers[/{id}]` manage existing providers, and
`POST /api/v1/oidc-providers/discover` runs the issuer probe. The client secret
is write-only: responses report `hasClientSecret` but never the value, and an
empty `clientSecret` on update keeps the current one. In `groupRoleMappings` the
role values are `org_admin` and `org_viewer`.

## Notes and limitations

- **Multiple providers per organization** are supported (one row each); a user
  reaches a specific one by email-domain discovery or a direct login URL.
- **Same email across organizations**: one account, one membership per org. The
  provider identity is namespaced per provider, so the same IdP subject can map
  cleanly across orgs.
- **Discovery caching**: OIDC discovery is cached (default 10 minutes, override
  with the `SSO_DISCOVERY_TTL` environment variable). The cache key includes a
  fingerprint of the client secret, so a secret rotation takes effect
  immediately; other issuer-metadata changes propagate within the TTL (per
  instance in multi-instance deployments).
- **Verified email required**: ID tokens without `email_verified: true` are
  rejected. Ensure your IdP issues verified emails for SSO users.
- **SAML 2.0** is not yet supported. See
  [SSO architecture](contributing/sso-architecture.md) for the seam where a new
  provider type would plug in.

## See also

- [Installation identity & accounts](identity-and-accounts.md) — disabling
  email/password login installation-wide, and deactivating/reactivating accounts.
- [SSO architecture](contributing/sso-architecture.md) (for contributors) — how
  the engine works and how to extend it.
