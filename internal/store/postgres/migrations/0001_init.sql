-- 0001_init: users and server-side sessions.
--
-- gen_random_uuid() is built into Postgres core since 13, so no extension
-- needed. Usernames are stored already-normalised (trimmed + lowercased) by
-- the app layer, so a plain UNIQUE is sufficient for case-insensitive logins.

CREATE TABLE users (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    username      text        NOT NULL UNIQUE,
    display       text        NOT NULL,
    password_hash text        NOT NULL DEFAULT '',
    created_at    timestamptz NOT NULL DEFAULT now()
);

-- Sessions are server-side: the row is the source of truth, the cookie only
-- carries the opaque id. Deleting the row is what makes logout real.
CREATE TABLE sessions (
    id         text        PRIMARY KEY,
    user_id    uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    last_seen  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX sessions_user_id_idx    ON sessions (user_id);
CREATE INDEX sessions_expires_at_idx ON sessions (expires_at);
