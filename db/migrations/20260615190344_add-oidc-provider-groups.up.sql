-- Group-based access control for per-org OIDC providers:
--   allowed_groups: IdP groups permitted to log in (empty = no restriction)
--   group_role_mappings: IdP group -> org role map for IdP-driven RBAC
ALTER TABLE organization_oidc_providers ADD COLUMN allowed_groups jsonb NOT NULL DEFAULT '[]';
ALTER TABLE organization_oidc_providers ADD COLUMN group_role_mappings jsonb NOT NULL DEFAULT '{}';
