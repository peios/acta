-- 0001_init: id generator, users, and server-side sessions.
--
-- Row ids are short, opaque, URL-safe strings (see internal/id) rather than
-- UUIDs: 8 chars from a 31-symbol lowercase alphabet with the ambiguous glyphs
-- removed, so they're easy to type and read. gen_id() is the column default;
-- ids aren't secrets (access is always authorised separately), so the built-in
-- PRNG is fine, and the app retries on the astronomically rare collision.
--
-- Usernames are stored already-normalised (trimmed + lowercased) by the app
-- layer, so a plain UNIQUE is sufficient for case-insensitive logins.

CREATE FUNCTION gen_id() RETURNS text
    LANGUAGE sql VOLATILE AS $$
    SELECT string_agg(
        substr('23456789abcdefghjkmnpqrstuvwxyz', 1 + floor(random() * 31)::int, 1),
        ''
    )
    FROM generate_series(1, 8);
$$;

CREATE TABLE users (
    id            text PRIMARY KEY DEFAULT gen_id(),
    username      text        NOT NULL UNIQUE,
    display       text        NOT NULL,
    password_hash text        NOT NULL DEFAULT '',
    created_at    timestamptz NOT NULL DEFAULT now()
);

-- Sessions are server-side: the row is the source of truth, the cookie only
-- carries the opaque id. Deleting the row is what makes logout real.
CREATE TABLE sessions (
    id         text        PRIMARY KEY,
    user_id    text        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    last_seen  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX sessions_user_id_idx    ON sessions (user_id);
CREATE INDEX sessions_expires_at_idx ON sessions (expires_at);
