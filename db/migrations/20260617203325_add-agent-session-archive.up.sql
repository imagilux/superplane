BEGIN;

-- Per-canvas agent session archive: relax the "one session per (org, user, canvas)"
-- rule to "one ACTIVE session + unlimited archived". The active session is the
-- single row with archived_at IS NULL; archiving stamps archived_at and a title,
-- and a fresh active session is provisioned in its place.

ALTER TABLE agent_sessions
    ADD COLUMN title TEXT NOT NULL DEFAULT '',
    ADD COLUMN archived_at TIMESTAMPTZ;

-- Replace the unconditional uniqueness with a partial unique index so only the
-- active (non-archived) session is constrained to one-per-canvas.
DROP INDEX IF EXISTS agent_sessions_user_canvas_idx;

CREATE UNIQUE INDEX agent_sessions_active_user_canvas_idx
    ON agent_sessions (organization_id, user_id, canvas_id)
    WHERE archived_at IS NULL;

-- Supports the archived-sessions drawer: list a canvas's archived sessions
-- for a user, newest-first, paginated.
CREATE INDEX agent_sessions_archived_idx
    ON agent_sessions (organization_id, user_id, canvas_id, created_at DESC)
    WHERE archived_at IS NOT NULL;

COMMIT;
