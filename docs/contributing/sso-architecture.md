# SSO architecture (per-organization OIDC)

This is the contributor's map of the per-organization OIDC Single Sign-On
feature: how the pieces fit, and how to extend them. For the admin/user guide
see [Single Sign-On](../single-sign-on.md).

## Two coexisting auth subsystems

1. **Global OAuth** (GitHub/Google) uses `goth`'s process-global registry,
   configured at boot. Untouched by this feature.
2. **Per-org generic OIDC** uses a **dynamic, per-request engine** in
   `pkg/authentication/sso`, because goth's global registry structurally can't
   do per-organization, runtime-configured discovery. This doc is about (2).

## The flow

```
/auth/sso/providers?email=…        home-realm discovery → which provider serves this domain
        │
        ▼
GET /auth/sso/{orgId}/{slug}        handleSSOLogin: load provider → registry.Get →
        │                           set signed state+nonce cookie → redirect to IdP authorize
        ▼  (user authenticates at IdP)
GET /auth/sso/{orgId}/{slug}/callback   handleSSOCallback: verify state cookie + CSRF →
                                    exchange code → verify ID token + nonce → require
                                    email_verified → domain gate → group gate → JIT
                                    provision + resolve role → issue session
```

All of (2)'s login/callback/discovery handlers are hand-written `net/http`
(mirroring the existing OAuth handlers), **not** gRPC. Provider CRUD is the
separate gRPC slice below.

## Where things live

| Concern | Location |
|---|---|
| OIDC engine (discovery/verifier cache) | `pkg/authentication/sso/registry.go` |
| Authenticator seam + ID-token claim mapping | `pkg/authentication/sso/authenticator.go` |
| Signed `state`+`nonce` cookie (stateless CSRF) | `pkg/authentication/sso/state.go` |
| SSRF guard for IdP fetches | `pkg/authentication/sso/httpguard.go` |
| Login / callback / discovery handlers | `pkg/authentication/sso_handler.go`, `sso_discovery.go` |
| Engine wiring + routes | `pkg/authentication/authentication.go` (`NewHandler`, `RegisterRoutes`) |
| Provider model + policy methods | `pkg/models/organization_oidc_provider.go` |
| Provider CRUD (gRPC) | `pkg/grpc/actions/oidcproviders/*`, `pkg/grpc/oidc_providers_service.go` |
| Proto | `protos/oidc_providers.proto` |
| RBAC resource (`oidc_providers`) | `rbac/rbac_org_policy.csv`, `pkg/authorization/interceptor.go` |
| Frontend | `web_src/src/pages/organization/settings/SingleSignOn*.tsx`, `hooks/useOidcProviders.ts` |

> The CRUD service and the engine live in **separate object graphs** — the
> gRPC `OIDCProvidersService` holds no reference to the engine `Registry`.

## The OIDC engine (`pkg/authentication/sso`)

- **`registry.go`** — `Registry.Get(ctx, Config) (*oauth2.Config, *oidc.IDTokenVerifier, error)`
  performs OIDC discovery (a network round-trip) on a cache miss and caches the
  result with a TTL (`SSO_DISCOVERY_TTL`, default 10m). `Config` is a plain
  struct **decoupled from the DB model** so the engine is independently testable.
  Discovery errors are returned but **not cached** (a flaky IdP can't poison the
  cache). The cache key includes a client-secret fingerprint, so a secret
  rotation yields a fresh entry.
- **`state.go`** — the CSRF `state` and OIDC `nonce` are carried in a single
  short-lived **signed cookie** (HS256 via the JWT signer secret). Stateless;
  mirrors the `account_token` model. Self-contained and reusable.
- **`httpguard.go`** — `NewGuardedHTTPClient` returns an `*http.Client` whose
  dialer rejects loopback, link-local (incl. `169.254.169.254`), unspecified,
  and multicast destinations **but allows RFC-1918** (self-hosted IdPs). It
  validates then dials the pinned IP (DNS-rebind safe), disables proxies, and
  caps redirects. **Any code that fetches an admin-supplied URL must route
  through this.** It's already reused by both the engine and the discovery probe.
- **`authenticator.go`** — the `Authenticator` interface (`AuthCodeURL` + `Complete`)
  and its OIDC implementation. `Complete` exchanges the code, verifies the ID
  token + nonce, and maps claims (read from a `map[string]any` keyed by the
  provider's configured groups claim) into a transport-agnostic `AuthResult`. This
  is the **seam the provider `type` dispatches to** — a future SAML type is another
  implementation here, not a branch in the HTTP handler. `AuthOptions` carries the
  optional `login_hint` and `prompt=none` request parameters, each gated by an
  installation setting at the call site.
- **`registry.go` also surfaces `end_session_endpoint`** from discovery via
  `EndSessionEndpoint`, which the handler's `idpLogoutURL` uses for RP-initiated
  logout.

## Data model

- `organization_oidc_providers` (`OrganizationOIDCProvider`): per-org provider
  config — `issuer_url`, `client_id`, AES-GCM-encrypted `client_secret_enc`
  (associated data = provider UUID), `scopes`, `allowed_email_domains`,
  `allowed_groups`, `group_role_mappings`, `groups_claim` (the ID-token claim to
  read groups from; default `groups`), `enabled`, soft-delete (the slug unique
  index is partial on `deleted_at IS NULL`, so slugs are reusable after delete).
  Policy logic is pure methods: `AllowsEmailDomain`, `AllowsGroups`, `ResolveRole`,
  `HasGroupFeatures`, `GroupsClaimOrDefault`.
- `accounts.deactivated_at` (`Account.IsDeactivated`): reversible disabled state.
- `installation_metadata`: installation toggles — `password_login_disabled` plus
  the SSO login-flow switches `sso_login_hint_enabled`, `sso_prompt_none_enabled`,
  `sso_auto_login_enabled`, and `sso_idp_logout_enabled` (the last carries an
  explicit `gorm:"column:sso_idp_logout_enabled"` tag, since GORM's namer would
  otherwise mangle "IdP").
- `account_providers` (`provider = "oidc:<providerUUID>"`): the linked external
  identity; an account with at least one row can authenticate without a password.
  The stored access and refresh tokens are AES-GCM-encrypted at rest (associated
  data = the account email).

## Claims, group gate, and role re-sync

The verified ID token is mapped to an `AuthResult` in `oidcAuthenticator.Complete`
(`sso/authenticator.go`), reading `email`, `email_verified`, `name`,
`preferred_username`, `picture`, and the provider's configured groups claim from a
`map[string]any` (`email_verified` accepts a JSON bool or the string `"true"`).
`handleSSOCallback` (`sso_handler.go`) then applies policy: `email_verified` is
required before any lookup, then the domain gate (`AllowsEmailDomain`) and group
gate (`AllowsGroups`) run. Role is `ResolveRole(groups)` (highest-precedence
mapping wins, default Viewer). When the provider has any group→role mapping, the
role is **re-synced every login** via `AssignRole` (which replaces the user's
existing grant) — never demoting an Owner.

## Installation auth gates (not OIDC-specific)

- **Deactivation** is enforced on **every** auth path: the session middleware
  (`getValidatedAccountFromCookie`), password login, the SSO/OAuth resolver
  (`findOrCreateAccountForProvider`), magic-code, and the bearer-token paths
  (`authenticateUserByToken` / `authenticateUserByScopedToken`). `Deactivate`
  also clears the account's token hashes so existing API tokens die immediately.
  When you add a *new* authentication path, it must reject deactivated accounts.
- **Disable-password-login** is gated by `guardPasswordLoginDisable`
  (`pkg/public/admin.go`): refuses to turn the toggle on if the acting admin has
  no non-password login, and (with confirmation) deactivates stranded
  password-only accounts in the same transaction.

## Extending it

**Add a new IdP / provider — easy, no code.** Any OIDC IdP that emits standard
`email` / `email_verified` / `groups` claims works through the CRUD API + the
"Verify issuer" discovery probe. Nothing to write.

**Custom groups claim — supported.** The groups claim name is configurable per
provider (`groups_claim` column, `groupsClaim` in the API; default `groups`).
`Complete` parses the ID token into a `map[string]any` and reads groups from the
configured claim, so an IdP that emits groups under `roles` / `memberOf` / a
namespaced name works without code changes — set the field and request whatever
scope releases that claim. Only the conventional `groups` claim has its scope
auto-requested. The other identity claims (`email`, `name`, `preferred_username`,
`picture`) are still read from their standard names.

**A new provider type (e.g. SAML) — the seam exists; SAML itself does not.** The
`Authenticator` interface is in place, `handleSSOCallback` drives the flow through
it, and `authenticatorFor` (`sso_handler.go`) **dispatches on `provider.Type`** —
the OIDC type returns an `oidcAuthenticator`, anything else is rejected. CRUD also
rejects `type=saml` up front via `resolveProviderType` (`oidcproviders/common.go`,
`Unimplemented`). What remains to add SAML:
1. Add the SAML-shaped columns (metadata URL/XML, SP entity ID, signing cert) to
   the model + a migration.
2. Implement a `samlAuthenticator` satisfying `Authenticator` (build the
   AuthnRequest in `AuthCodeURL`, verify the assertion in `Complete`).
3. Add a `case` in `authenticatorFor` for the SAML type and relax the CRUD
   rejection. The handler's policy pipeline (gates, JIT, role) is reused unchanged.

**Reuse the engine elsewhere — primitives and the Authenticator yes, the
provisioning pipeline no.** `Registry`, `state`, `httpguard`, and the
`Authenticator`/`AuthResult` seam are reusable as-is. The
claim→account→role→session provisioning pipeline, however, still lives inside
`handleSSOCallback` and is not extracted, so a second entrypoint would
re-implement that part.

## Known limitations / roadmap

- The groups claim name is configurable (`groups_claim`); the other identity claim
  names (`email`, `name`, `preferred_username`, `picture`) are still standard-named.
- The `Authenticator` seam and `type` dispatch exist, but only OIDC is
  implemented — SAML (columns + assertion handling) and SCIM are not.
- The CRUD service and engine `Registry` are disjoint, so `Registry.Invalidate`
  is **not** wired into provider edits. Correctness instead rides on the cache
  **key** (issuer / client ID / redirect / secret-fingerprint all participate, so
  those edits self-invalidate) plus a per-request DB read of `enabled` and the
  groups claim. Only a *scopes* edit can be served stale, up to the TTL;
  multi-instance deployments converge via the TTL regardless.
