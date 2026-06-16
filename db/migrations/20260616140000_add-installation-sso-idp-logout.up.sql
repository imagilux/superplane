-- Installation-admin toggle: on sign-out, also end the session at the IdP
-- (RP-initiated OIDC logout) so single-logout propagates. Independent of the
-- other SSO login options.
ALTER TABLE installation_metadata
  ADD COLUMN sso_idp_logout_enabled boolean NOT NULL DEFAULT false;
