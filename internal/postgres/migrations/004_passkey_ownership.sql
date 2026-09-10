-- A credential ID belongs to exactly one account at this relying party. Public
-- key material remains in the encrypted, domain-owned security record.
CREATE TABLE passkey_owners (
 credential_id bytea PRIMARY KEY,
 account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE
);
CREATE INDEX passkey_owners_account ON passkey_owners(account_id);
