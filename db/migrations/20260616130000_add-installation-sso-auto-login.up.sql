-- Installation-admin toggle for unattended SSO auto-login: when enabled (and
-- prompt=none is allowed and exactly one SSO provider is configured), the login
-- page silently attempts SSO so a user with a live IdP session never sees a
-- login screen.
ALTER TABLE installation_metadata
  ADD COLUMN sso_auto_login_enabled boolean NOT NULL DEFAULT false;
