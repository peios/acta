CREATE TABLE accounts (
 id uuid PRIMARY KEY,
 username text COLLATE "C" NOT NULL CHECK (username ~ '^[a-z0-9]([a-z0-9._-]{0,30}[a-z0-9])?$'),
 parent_id uuid REFERENCES accounts(id) ON DELETE RESTRICT,
 display_name text,
 is_admin boolean NOT NULL DEFAULT false,
 disabled_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now(),
 CONSTRAINT account_namespace_unique UNIQUE NULLS NOT DISTINCT (parent_id, username),
 CHECK (parent_id IS NULL OR (parent_id <> id AND NOT is_admin))
);
-- Root and direct-child names are the only agreed namespace shape. Lock the
-- parent while checking, so concurrent structural updates cannot evade it.
CREATE FUNCTION enforce_account_parent() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP = 'UPDATE' AND NEW.parent_id IS DISTINCT FROM OLD.parent_id THEN
  RAISE EXCEPTION 'account parentage cannot be changed' USING ERRCODE = '23514';
 END IF;
 IF NEW.parent_id IS NOT NULL THEN
  PERFORM id FROM accounts WHERE id = NEW.parent_id AND parent_id IS NULL FOR UPDATE;
  IF NOT FOUND THEN
   RAISE EXCEPTION 'an agent parent must be a root account' USING ERRCODE = '23514';
  END IF;
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER account_parent BEFORE INSERT OR UPDATE OF parent_id ON accounts
 FOR EACH ROW EXECUTE FUNCTION enforce_account_parent();

CREATE TABLE password_credentials (
 account_id uuid PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
 password_hash text NOT NULL
);
CREATE TABLE installation (
 singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
 completed_at timestamptz,
 completed_by uuid REFERENCES accounts(id) ON DELETE RESTRICT,
 CHECK ((completed_at IS NULL) = (completed_by IS NULL))
);
INSERT INTO installation (singleton) VALUES (true);
CREATE TABLE setup_grants (
 token_hash bytea PRIMARY KEY CHECK (octet_length(token_hash) = 32),
 expires_at timestamptz NOT NULL
);
CREATE TABLE browser_sessions (
 token_hash bytea PRIMARY KEY CHECK (octet_length(token_hash) = 32),
 account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
 created_at timestamptz NOT NULL,
 last_seen_at timestamptz NOT NULL,
 expires_at timestamptz NOT NULL,
 CHECK (last_seen_at >= created_at AND expires_at > created_at)
);
CREATE INDEX browser_sessions_account ON browser_sessions(account_id);
CREATE INDEX browser_sessions_expiry ON browser_sessions(expires_at);
CREATE TABLE authentication_attempts (
 bucket_hash bytea PRIMARY KEY CHECK (octet_length(bucket_hash) = 32),
 attempts integer NOT NULL CHECK (attempts > 0),
 expires_at timestamptz NOT NULL
);
CREATE INDEX authentication_attempts_expiry ON authentication_attempts(expires_at);
CREATE TABLE account_events (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 account_id uuid REFERENCES accounts(id) ON DELETE RESTRICT,
 kind text NOT NULL,
 occurred_at timestamptz NOT NULL
);
