# Installation identity & accounts

These are **installation-wide** controls for an *installation administrator*
(the `Installation Admin` area, distinct from per-organization Owner/Admin
roles). They govern how people can sign in and let you disable individual
accounts. Non-admins can't see these screens.

## Disable email/password login

By default an installation offers email/password login alongside SSO and the
GitHub/Google OAuth buttons. Once everyone signs in through SSO, you can turn
the email/password form off installation-wide.

**Where:** Installation Admin → **Settings** → **Identity** card → *Disable
email/password login*.

When enabled, the email/password form is hidden and the server refuses password
logins and password sign-ups. SSO and existing sessions are unaffected. This is
independent of `BLOCK_SIGNUP` (which only blocks new sign-ups).

### Lockout guard

You can't turn this on if it would lock you out. The toggle is **greyed out**
(with the reason shown) unless **your own account can sign in without a
password** — i.e. it has an SSO/OAuth identity linked. Only an installation
admin can ever turn the setting back off, so if every admin were password-only,
nobody could re-enable it.

If the toggle is blocked, either:

- sign in once via your organization's SSO so your account gains an external
  identity, then retry; or
- in **Installation Admin → Accounts**, promote an SSO-capable account to
  installation admin and use that one.

### Stranded password-only accounts

Disabling password login strands any account that can *only* sign in with a
password (it has a password and no SSO/OAuth identity). When you turn the toggle
on, SuperPlane lists those accounts and asks you to confirm; on confirmation it
**deactivates** them (see below — they are disabled, not deleted) and disables
the form in a single step. Re-enabling password login later does **not**
automatically reactivate them; reactivate from the Accounts page if needed.

> Tip: to keep a non-SSO "break-glass" admin after going SSO-only, give that
> account an SSO identity first (so it isn't password-only and won't be
> deactivated).

## Account deactivation

Deactivation is a **reversible** "disabled" state — not a deletion. A deactivated
account keeps all its data, memberships, and history but **cannot authenticate
on any path**: web session, email/password, SSO/OAuth, magic-code, and API /
personal-access tokens are all rejected, and deactivation clears the account's
API token hashes so existing tokens stop working immediately. SSO cannot revive
a deactivated account by signing in.

**Where:** Installation Admin → **Accounts**. Each row shows a **Disabled** badge
when deactivated, and a **Deactivate** / **Reactivate** action:

- **Deactivate** asks for confirmation, then disables the account.
- **Reactivate** re-enables it (the user signs in fresh; any old API tokens were
  cleared and must be regenerated).

You cannot deactivate your own account (the row shows "Cannot change own
access"). Service accounts are unaffected — they have their own token lifecycle.

### Via the API

```
POST /admin/api/accounts/{id}/deactivate
POST /admin/api/accounts/{id}/reactivate
```

Both require an authenticated installation admin (non-admins receive `404`).
Deactivating your own account returns `400`.

## See also

- [Single Sign-On](single-sign-on.md) — configuring per-org OIDC providers.
