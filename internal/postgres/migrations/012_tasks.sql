CREATE TABLE task_settings (
 workspace_id uuid PRIMARY KEY REFERENCES workspaces(id),
 prefix text COLLATE "C" NOT NULL UNIQUE CHECK(prefix ~ '^[A-Z]{2,10}$'),
 next_number bigint NOT NULL DEFAULT 1,
 creation_status uuid NOT NULL,
 completed_status uuid NOT NULL CHECK(creation_status<>completed_status),
 version bigint NOT NULL DEFAULT 1,
 revision bigint NOT NULL DEFAULT 1
);
CREATE TABLE task_prefixes(prefix text COLLATE "C" PRIMARY KEY,workspace_id uuid NOT NULL REFERENCES workspaces(id));
CREATE TABLE task_statuses(id uuid PRIMARY KEY,workspace_id uuid NOT NULL REFERENCES workspaces(id),name text NOT NULL,position integer NOT NULL,UNIQUE(workspace_id,id));
CREATE UNIQUE INDEX task_status_name ON task_statuses(workspace_id,lower(name));
ALTER TABLE task_settings ADD FOREIGN KEY(workspace_id,creation_status) REFERENCES task_statuses(workspace_id,id) DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE task_settings ADD FOREIGN KEY(workspace_id,completed_status) REFERENCES task_statuses(workspace_id,id) DEFERRABLE INITIALLY DEFERRED;
CREATE FUNCTION claim_task_prefix() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO task_prefixes(prefix,workspace_id) VALUES(NEW.prefix,NEW.workspace_id)
 ON CONFLICT(prefix) DO UPDATE SET workspace_id=EXCLUDED.workspace_id WHERE task_prefixes.workspace_id=EXCLUDED.workspace_id;
 IF NOT FOUND THEN RAISE EXCEPTION 'prefix reserved' USING ERRCODE='23505',CONSTRAINT='task_prefixes_pkey'; END IF;
 RETURN NEW;
END; $$;
CREATE TRIGGER task_prefix_claim AFTER INSERT OR UPDATE OF prefix ON task_settings FOR EACH ROW EXECUTE FUNCTION claim_task_prefix();
CREATE FUNCTION initialize_workspace_tasks() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE p text; base text; n bigint:=0; suffix text; k bigint; a uuid:=gen_random_uuid(); b uuid:=gen_random_uuid(); c uuid:=gen_random_uuid();
BEGIN
 base:=left(regexp_replace(upper(NEW.slug),'[^A-Z]','','g'),6);IF length(base)<2 THEN base:='WS'; END IF;p:=base;
 WHILE EXISTS(SELECT 1 FROM task_prefixes WHERE prefix=p) LOOP
 n:=n+1;k:=n;suffix:='';WHILE k>0 LOOP suffix:=chr(65+((k-1)%26)::int)||suffix;k:=(k-1)/26;END LOOP;p:=base||suffix;
 END LOOP;
 INSERT INTO task_statuses VALUES(a,NEW.id,'To do',0),(b,NEW.id,'In progress',1),(c,NEW.id,'Done',2);
 INSERT INTO task_settings(workspace_id,prefix,creation_status,completed_status) VALUES(NEW.id,p,a,c);
 RETURN NEW;
END; $$;
CREATE TRIGGER workspace_task_defaults AFTER INSERT ON workspaces FOR EACH ROW EXECUTE FUNCTION initialize_workspace_tasks();
-- Invoke the same initializer for existing workspaces.
CREATE TRIGGER existing_workspace_task_defaults AFTER UPDATE OF slug ON workspaces FOR EACH ROW EXECUTE FUNCTION initialize_workspace_tasks();
UPDATE workspaces SET slug=slug;
DROP TRIGGER existing_workspace_task_defaults ON workspaces;
CREATE TABLE tasks (
 id uuid PRIMARY KEY,workspace_id uuid NOT NULL REFERENCES workspaces(id),number bigint NOT NULL,
 title text NOT NULL,description text NOT NULL DEFAULT '',status_id uuid NOT NULL,parent_id uuid,
 title_version bigint NOT NULL DEFAULT 1,description_version bigint NOT NULL DEFAULT 1,status_version bigint NOT NULL DEFAULT 1,parent_version bigint NOT NULL DEFAULT 1,assignees_version bigint NOT NULL DEFAULT 1,
 created_by uuid NOT NULL REFERENCES accounts(id),created_at timestamptz NOT NULL,updated_at timestamptz NOT NULL,
 UNIQUE(workspace_id,number),UNIQUE(workspace_id,id),CHECK(parent_id IS NULL OR parent_id<>id),
 FOREIGN KEY(workspace_id,status_id) REFERENCES task_statuses(workspace_id,id) DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(workspace_id,parent_id) REFERENCES tasks(workspace_id,id) DEFERRABLE INITIALLY DEFERRED
);
CREATE INDEX task_parent ON tasks(workspace_id,parent_id,number DESC);
CREATE INDEX task_status ON tasks(workspace_id,status_id,number DESC);
CREATE TABLE task_assignees(task_id uuid NOT NULL REFERENCES tasks(id),account_id uuid NOT NULL REFERENCES accounts(id),PRIMARY KEY(task_id,account_id));
CREATE INDEX task_account ON task_assignees(account_id,task_id);
