-- 0033_agent_sessions: browser-driven agent sessions (Claude Code today, other
-- backends later). A session's id is a UUID minted by Acta and handed to the
-- harness, which passes it straight through as the backend's own session id —
-- so Acta's id and the backend's are the same string and nothing needs a
-- mapping. The transcript is an append-only log of wire frames stored verbatim
-- (jsonb), one row per frame in arrival order; the browser renders from these
-- and a session whose harness is offline still has readable history.
CREATE TABLE agent_sessions (
    id         text        PRIMARY KEY,
    owner_id   text        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    backend    text        NOT NULL,
    cwd        text        NOT NULL DEFAULT '',
    title      text        NOT NULL DEFAULT '',
    options    jsonb       NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX agent_sessions_owner_idx ON agent_sessions (owner_id, updated_at DESC);

CREATE TABLE agent_session_events (
    seq        bigserial   PRIMARY KEY,
    session_id text        NOT NULL REFERENCES agent_sessions(id) ON DELETE CASCADE,
    kind       text        NOT NULL,
    payload    jsonb       NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX agent_session_events_session_idx ON agent_session_events (session_id, seq);
