-- 0009_api_tokens: personal access tokens.
--
-- A token authenticates as its owning user with full authority — no scopes and
-- no expiry in v1; revocation is deletion. Only the SHA-256 hash of the token
-- is stored, so a database leak does not yield usable credentials; the
-- plaintext is shown to the user exactly once, at creation. `prefix` keeps the
-- leading, non-secret characters ("acta_pat_AbCd1234") so a token is
-- identifiable in the account UI and in logs without revealing the secret.

CREATE TABLE api_tokens (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name         text        NOT NULL DEFAULT '',
    token_hash   bytea       NOT NULL UNIQUE,
    prefix       text        NOT NULL DEFAULT '',
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz
);

CREATE INDEX api_tokens_user_id_idx ON api_tokens (user_id);
