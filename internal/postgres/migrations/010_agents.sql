-- Namespace segments continue to reserve names within an immutable owner.
-- Full handles preserve historical references when an owner is renamed.
CREATE TABLE account_handles (
 handle text COLLATE "C" PRIMARY KEY,
 account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT
);
INSERT INTO account_handles(handle,account_id)
 SELECT CASE WHEN n.parent_id IS NULL THEN n.username ELSE p.username || '/' || n.username END,n.account_id
 FROM account_names n LEFT JOIN accounts p ON p.id=n.parent_id;
CREATE INDEX account_handles_owner ON account_handles(account_id);
CREATE FUNCTION reserve_account_handle(value text, account uuid) RETURNS void LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO account_handles(handle,account_id) VALUES(value,account)
 ON CONFLICT(handle) DO UPDATE SET account_id=EXCLUDED.account_id WHERE account_handles.account_id=EXCLUDED.account_id;
 IF NOT FOUND THEN
  RAISE EXCEPTION 'username is in use or reserved' USING ERRCODE='23505',CONSTRAINT='account_handles_pkey';
 END IF;
END;
$$;
CREATE FUNCTION claim_account_handle() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE owner_name text; child record;
BEGIN
 IF NEW.parent_id IS NULL THEN
  PERFORM reserve_account_handle(NEW.username,NEW.id);
  IF TG_OP='UPDATE' AND NEW.username IS DISTINCT FROM OLD.username THEN
   FOR child IN SELECT id,username FROM accounts WHERE parent_id=NEW.id ORDER BY id FOR UPDATE LOOP
    PERFORM reserve_account_handle(NEW.username || '/' || child.username,child.id);
    UPDATE accounts SET profile_version=profile_version+1 WHERE id=child.id;
   END LOOP;
  END IF;
 ELSE
  SELECT username INTO owner_name FROM accounts WHERE id=NEW.parent_id;
  PERFORM reserve_account_handle(owner_name || '/' || NEW.username,NEW.id);
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER account_handle_claim AFTER INSERT OR UPDATE OF username ON accounts FOR EACH ROW EXECUTE FUNCTION claim_account_handle();
ALTER TABLE accounts ADD CONSTRAINT agent_identity_only CHECK(parent_id IS NULL OR (NOT pending AND NOT require_mfa AND NOT mfa_enrolled AND NOT ('site.superuser'=ANY(direct_permissions))));
CREATE FUNCTION require_human_account() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 PERFORM id FROM accounts WHERE id=NEW.account_id AND parent_id IS NULL FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'this feature requires a human account' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER password_human BEFORE INSERT OR UPDATE ON password_credentials FOR EACH ROW EXECUTE FUNCTION require_human_account();
CREATE TRIGGER passkey_human BEFORE INSERT OR UPDATE ON passkey_owners FOR EACH ROW EXECUTE FUNCTION require_human_account();
CREATE TRIGGER group_human BEFORE INSERT OR UPDATE ON group_memberships FOR EACH ROW EXECUTE FUNCTION require_human_account();
CREATE TRIGGER account_link_human BEFORE INSERT OR UPDATE ON account_links FOR EACH ROW EXECUTE FUNCTION require_human_account();
ALTER TABLE browser_sessions ADD COLUMN authorized_by uuid REFERENCES accounts(id) ON DELETE RESTRICT;
UPDATE browser_sessions SET authorized_by=account_id;
ALTER TABLE browser_sessions ALTER COLUMN authorized_by SET NOT NULL;
-- Keep legacy/browser insert paths using the subject as their authorizer.
CREATE FUNCTION session_authorizer() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE owner uuid;
BEGIN
 SELECT parent_id INTO owner FROM accounts WHERE id=NEW.account_id;
 IF NEW.authorized_by IS NULL THEN NEW.authorized_by=NEW.account_id; END IF;
 IF owner IS NOT NULL AND (NEW.kind='browser' OR NEW.authorized_by<>owner) THEN
  RAISE EXCEPTION 'agent sessions require owner authorization' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER session_authorizer BEFORE INSERT OR UPDATE ON browser_sessions FOR EACH ROW EXECUTE FUNCTION session_authorizer();
