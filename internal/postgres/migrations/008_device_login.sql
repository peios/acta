CREATE TABLE device_requests (
 token_hash bytea PRIMARY KEY,
 code_hash bytea NOT NULL UNIQUE,
 description text NOT NULL,
 account_id uuid REFERENCES accounts(id) ON DELETE CASCADE,
 state text NOT NULL CHECK(state IN ('pending','approved','denied','consumed')),
 expires_at timestamptz NOT NULL,
 encrypted bytea
);
CREATE INDEX device_expiry ON device_requests(expires_at);
