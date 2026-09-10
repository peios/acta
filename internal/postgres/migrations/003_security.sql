ALTER TABLE accounts ADD COLUMN security_version bigint NOT NULL DEFAULT 1 CHECK (security_version>0);
ALTER TABLE accounts ADD COLUMN security_changed_at timestamptz NOT NULL DEFAULT now();
ALTER TABLE accounts ADD COLUMN security_data bytea;
ALTER TABLE browser_sessions ADD COLUMN id uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE;
ALTER TABLE browser_sessions ADD COLUMN description text NOT NULL DEFAULT 'Browser session';
ALTER TABLE browser_sessions ADD COLUMN proof jsonb NOT NULL DEFAULT '{}';
CREATE TABLE security_flows (
 token_hash bytea PRIMARY KEY CHECK (octet_length(token_hash)=32),
 account_id uuid REFERENCES accounts(id) ON DELETE CASCADE,
 expires_at timestamptz NOT NULL,
 encrypted bytea NOT NULL
);
CREATE INDEX security_flows_expiry ON security_flows(expires_at);
CREATE TABLE security_key (
 singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
 fingerprint bytea NOT NULL
);
