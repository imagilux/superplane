-- Installation-wide, admin-configured AI agent provider. Lets an installation
-- admin point the managed agents at an OpenAI-compatible endpoint from the
-- settings UI instead of (or in addition to) the AGENT_* environment variables.
-- agent_provider is '' (use the environment default), 'anthropic' (managed; its
-- secrets stay in the environment), or 'openai' (the endpoint configured here).
-- The API key is stored encrypted at rest in agent_api_key_enc.
ALTER TABLE installation_metadata
  ADD COLUMN agent_provider varchar(50) NOT NULL DEFAULT '',
  ADD COLUMN agent_base_url text NOT NULL DEFAULT '',
  ADD COLUMN agent_model varchar(255) NOT NULL DEFAULT '',
  ADD COLUMN agent_api_key_enc text NOT NULL DEFAULT '';
