CREATE TABLE guide_preferences (
 scope_key text PRIMARY KEY,
 account_id uuid UNIQUE REFERENCES accounts(id) ON DELETE CASCADE,
 content text NOT NULL CHECK (char_length(content) <= 8000),
 revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
 updated_by uuid NOT NULL REFERENCES accounts(id),
 updated_at timestamptz NOT NULL DEFAULT now(),
 CHECK ((scope_key = 'site' AND account_id IS NULL) OR (account_id IS NOT NULL AND scope_key = account_id::text))
);
