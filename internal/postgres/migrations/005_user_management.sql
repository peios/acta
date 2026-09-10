ALTER TABLE accounts ADD COLUMN pending boolean NOT NULL DEFAULT false;
CREATE TABLE account_links (
 account_id uuid PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
 token_hash bytea UNIQUE NOT NULL CHECK(octet_length(token_hash)=32),
 purpose text NOT NULL CHECK(purpose IN ('invite','reset')),
 expires_at timestamptz NOT NULL,
 security_version bigint NOT NULL,
 disable_mfa boolean NOT NULL DEFAULT false,
 remove_passkeys boolean NOT NULL DEFAULT false
);
-- Revoked tokens retain only enough information to explain a disabled account.
-- They never authorize access, even after the account is enabled again.
CREATE TABLE disabled_session_notices (
 token_hash bytea PRIMARY KEY,
 account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
 expires_at timestamptz NOT NULL
);
ALTER TABLE account_events ADD COLUMN actor_id uuid REFERENCES accounts(id);
