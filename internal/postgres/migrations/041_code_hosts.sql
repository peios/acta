CREATE TABLE code_hosts (
 owner_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
 id uuid NOT NULL,
 instance_id uuid NOT NULL,
 session_hash bytea REFERENCES browser_sessions(token_hash) ON DELETE SET NULL,
 name text NOT NULL CHECK (octet_length(name) BETWEEN 1 AND 100),
 os text NOT NULL CHECK (octet_length(os) BETWEEN 1 AND 32),
 arch text NOT NULL CHECK (octet_length(arch) BETWEEN 1 AND 32),
 last_seen_at timestamptz NOT NULL,
 lease_until timestamptz NOT NULL,
 PRIMARY KEY (owner_id, id)
);
