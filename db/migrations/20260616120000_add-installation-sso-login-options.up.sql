-- Installation-admin toggles for optional OIDC authorization-request parameters
-- on the per-org SSO login flow:
--   sso_login_hint_enabled  -> forward the discovery email as `login_hint`
--   sso_prompt_none_enabled -> allow `prompt=none` (silent authentication)
ALTER TABLE installation_metadata
  ADD COLUMN sso_login_hint_enabled boolean NOT NULL DEFAULT false,
  ADD COLUMN sso_prompt_none_enabled boolean NOT NULL DEFAULT false;
