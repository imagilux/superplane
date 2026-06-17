-- Per-organization, admin-configured generic OIDC identity providers used for
-- Single Sign-On. An organization may have multiple providers. The client secret
-- is stored encrypted at rest (base64 of AES-GCM ciphertext) in client_secret_enc.
CREATE TABLE organization_oidc_providers (
  id                    uuid NOT NULL DEFAULT gen_random_uuid(),
  organization_id       uuid NOT NULL REFERENCES organizations(id),
  slug                  varchar(64) NOT NULL,
  display_name          varchar(255) NOT NULL,
  type                  varchar(50) NOT NULL DEFAULT 'oidc',
  issuer_url            text NOT NULL,
  client_id             text NOT NULL,
  client_secret_enc     text NOT NULL,
  scopes                jsonb NOT NULL DEFAULT '["openid","email","profile"]',
  allowed_email_domains jsonb NOT NULL DEFAULT '[]',
  enabled               boolean NOT NULL DEFAULT true,
  created_by            uuid REFERENCES users(id),
  created_at            timestamp NOT NULL DEFAULT now(),
  updated_at            timestamp NOT NULL DEFAULT now(),
  deleted_at            timestamp,
  PRIMARY KEY (id)
);

-- Slug is unique per organization (ignoring soft-deleted rows so a slug can be reused).
CREATE UNIQUE INDEX uniq_oidc_provider_slug_per_org
  ON organization_oidc_providers (organization_id, slug)
  WHERE deleted_at IS NULL;

CREATE INDEX idx_oidc_providers_org
  ON organization_oidc_providers (organization_id)
  WHERE deleted_at IS NULL;

-- Supports home-realm discovery: allowed_email_domains @> '["domain"]' containment lookups.
CREATE INDEX idx_oidc_providers_allowed_domains
  ON organization_oidc_providers USING gin (allowed_email_domains)
  WHERE deleted_at IS NULL;
