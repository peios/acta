ALTER TABLE browser_sessions ADD COLUMN kind text NOT NULL DEFAULT 'browser' CHECK (kind IN ('browser','cli','mcp'));
ALTER TABLE browser_sessions ADD COLUMN tool_grants text[] NOT NULL DEFAULT '{}';
UPDATE browser_sessions SET kind='cli' WHERE description LIKE 'CLI · %';
CREATE TABLE oauth_clients (
 id text PRIMARY KEY,
 metadata jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE oauth_requests (
 request_hash bytea PRIMARY KEY,
 code_hash bytea UNIQUE,
 data jsonb NOT NULL,
 expires_at timestamptz NOT NULL
);
CREATE INDEX oauth_request_expiry ON oauth_requests(expires_at);
CREATE TABLE oauth_tokens (
 token_hash bytea PRIMARY KEY,
 session_id uuid NOT NULL REFERENCES browser_sessions(id) ON DELETE CASCADE,
 client_id text NOT NULL REFERENCES oauth_clients(id),
 resource text NOT NULL,
 kind text NOT NULL CHECK(kind IN ('access','refresh')),
 expires_at timestamptz NOT NULL,
 used boolean NOT NULL DEFAULT false
);
CREATE INDEX oauth_session_tokens ON oauth_tokens(session_id);
CREATE INDEX oauth_token_expiry ON oauth_tokens(expires_at);
