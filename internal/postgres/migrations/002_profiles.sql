ALTER TABLE accounts ADD COLUMN profile_version bigint NOT NULL DEFAULT 1 CHECK (profile_version > 0);

-- Current and historical names occupy the same namespace. Currentness is
-- determined by accounts.username; a rename never deletes its previous claim.
CREATE TABLE account_names (
 parent_id uuid REFERENCES accounts(id) ON DELETE RESTRICT,
 username text COLLATE "C" NOT NULL CHECK (username ~ '^[a-z0-9]([a-z0-9._-]{0,30}[a-z0-9])?$'),
 account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
 CONSTRAINT account_name_unique UNIQUE NULLS NOT DISTINCT (parent_id, username)
);
CREATE INDEX account_names_owner ON account_names(account_id);
INSERT INTO account_names(parent_id, username, account_id) SELECT parent_id, username, id FROM accounts;

-- This single claim point also protects future account creation paths. The
-- unique constraint arbitrates competing claims across server instances.
CREATE FUNCTION claim_account_name() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO account_names(parent_id, username, account_id)
 VALUES(NEW.parent_id, NEW.username, NEW.id)
 ON CONFLICT ON CONSTRAINT account_name_unique DO UPDATE
 SET account_id = EXCLUDED.account_id
 WHERE account_names.account_id = EXCLUDED.account_id;
 IF NOT FOUND THEN
  RAISE EXCEPTION 'username is in use or reserved'
   USING ERRCODE = '23505', CONSTRAINT = 'account_name_unique';
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER account_name_claim AFTER INSERT OR UPDATE OF username ON accounts
 FOR EACH ROW EXECUTE FUNCTION claim_account_name();
