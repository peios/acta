-- 0002_passkeys: WebAuthn credentials and short-lived ceremony challenges.

CREATE TABLE credentials (
    id            uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    credential_id bytea       NOT NULL UNIQUE,
    public_key    bytea       NOT NULL,
    sign_count    bigint      NOT NULL DEFAULT 0,
    transports    text[]      NOT NULL DEFAULT '{}',
    aaguid        bytea       NOT NULL DEFAULT '',
    name          text        NOT NULL DEFAULT '',
    created_at    timestamptz NOT NULL DEFAULT now(),
    last_used_at  timestamptz
);

CREATE INDEX credentials_user_id_idx ON credentials (user_id);

-- Ceremony state held between begin and finish. Single-use and short-lived;
-- the id is also carried to the client in a brief cookie.
CREATE TABLE webauthn_challenges (
    id         text        PRIMARY KEY,
    user_id    uuid        REFERENCES users(id) ON DELETE CASCADE,
    data       jsonb       NOT NULL,
    expires_at timestamptz NOT NULL
);
