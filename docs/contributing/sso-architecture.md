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

## Data model

- `organization_oidc_providers` (`OrganizationOIDCProvider`): per-org provider
  config — `issuer_url`, `client_id`, AES-GCM-encrypted `client_secret_enc`
  (associated data = provider UUID), `scopes`, `allowed_email_domains`,
  `allowed_groups`, `group_role_mappings`, `enabled`, soft-delete. Policy logic
  is pure methods: `AllowsEmailDomain`, `AllowsGroups`, `ResolveRole`,
  `HasGroupFeatures`.
- `accounts.deactivated_at` (`Account.IsDeactivated`): reversible disabled state.
- `installation_metadata.password_login_disabled`: the installation toggle.
- `account_providers` (`provider = "oidc:<providerUUID>"`): the linked external
  identity; an account with at least one row can authenticate without a password.

## Claims, group gate, and role re-sync

In `handleSSOCallback` (`sso_handler.go`): the verified ID token is parsed into a
fixed claim struct (`email`, `email_verified`, `name`, `preferred_username`,
`picture`, `groups`). `email_verified` is required before any lookup. Then the
domain gate (`AllowsEmailDomain`) and group gate (`AllowsGroups`) run. Role is
`ResolveRole(groups)` (highest-precedence mapping wins, default Viewer). When the
provider has any group→role mapping, the role is **re-synced every login** via
`AssignRole` (which replaces the user's existing grant) — never demoting an
Owner.

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

**Custom claim mapping — currently invasive.** The claim names are hardcoded in
the callback's claim struct (notably the `groups` claim and the `"groups"`
scope, in `sso_handler.go`). An IdP that emits `roles` / `memberOf` / namespaced
or object-ID groups can't be mapped without editing Go. The clean fix is to add
configurable claim-name columns (e.g. `groups_claim`) and parse the ID token
into a `map[string]any` keyed by the configured names. **Recommended before
relying on Okta/Entra group features.**

**A new provider type (e.g. SAML) — the `type` column is currently a placeholder,
not a working seam.** `resolveProviderType` (`oidcproviders/common.go`) is the
single chokepoint and returns `Unimplemented` for SAML. But there is **no
dispatch on `type`** in the flow (the handler filters out non-OIDC), no
`Authenticator` interface for `type` to resolve to, and the schema is
OIDC-shaped (no SAML metadata/cert columns). To add SAML you must first build the
seam:
1. Extract an `Authenticator` interface in `pkg/authentication/sso`, e.g.
   `Complete(ctx, code/assertion, state) (*AuthResult, error)`, and make the
   current OIDC flow its first implementation; have `handleSSOCallback` call it
   and keep only HTTP concerns (cookies/redirects/session) in the handler.
2. Add the SAML-shaped columns (metadata URL/XML, SP entity ID, signing cert).
3. Dispatch on `type` to the right `Authenticator` constructor.

**Reuse the engine elsewhere — primitives yes, pipeline no.** `Registry`,
`state`, and `httpguard` are reusable as-is. The claim→account→role pipeline,
however, lives inside `handleSSOCallback` and is not extracted, so a second
entrypoint would re-implement it — the same `Authenticator`/`AuthResult` seam
above is the fix.

## Known limitations / roadmap

- Claim names are hardcoded (`groups`) — not configurable per provider.
- The `type` column is cosmetic until the `Authenticator` seam exists; SAML and
  SCIM are not implemented.
- The CRUD service and engine `Registry` are disjoint; cross-instance cache
  invalidation converges via the TTL (secret rotation is handled by the cache
  key fingerprint).
