-- Per-organization, admin-configured AI agent backends (OpenAI-compatible
-- endpoints). An organization may configure several; the enabled one is used for
-- its agent sessions. The API key is stored encrypted at rest in api_key_enc.
CREATE TABLE organization_agent_providers (
  id              uuid NOT NULL DEFAULT gen_random_uuid(),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  slug            varchar(64) NOT NULL,
  display_name    varchar(255) NOT NULL,
  type            varchar(50) NOT NULL DEFAULT 'openai',
  base_url        text NOT NULL,
  model           varchar(255) NOT NULL,
  api_key_enc     text NOT NULL DEFAULT '',
  enabled         boolean NOT NULL DEFAULT true,
  created_by      uuid REFERENCES users(id),
  created_at      timestamp NOT NULL DEFAULT now(),
  updated_at      timestamp NOT NULL DEFAULT now(),
  deleted_at      timestamp,
  PRIMARY KEY (id)
);

-- Slug is unique per organization (ignoring soft-deleted rows so a slug can be reused).
CREATE UNIQUE INDEX uniq_agent_provider_slug_per_org
  ON organization_agent_providers (organization_id, slug)
  WHERE deleted_at IS NULL;

CREATE INDEX idx_agent_providers_org
  ON organization_agent_providers (organization_id)
  WHERE deleted_at IS NULL;
