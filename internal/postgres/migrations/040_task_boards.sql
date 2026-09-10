-- Board membership is derived from status: there is no second task field to drift.
CREATE TABLE task_boards (
 workspace_id uuid NOT NULL REFERENCES workspaces(id),
 slug text NOT NULL CHECK(slug IN ('tasks','backlog')),
 name text NOT NULL,
 position integer NOT NULL,
 creation_status uuid NOT NULL,
 completed_status uuid,
 PRIMARY KEY(workspace_id,slug),
 CHECK(completed_status IS NULL OR creation_status<>completed_status)
);
ALTER TABLE task_statuses ADD COLUMN board text NOT NULL DEFAULT 'tasks';
DROP INDEX task_status_name;
CREATE UNIQUE INDEX task_status_name ON task_statuses(workspace_id,board,lower(name));
INSERT INTO task_boards SELECT workspace_id,'tasks','Tasks',0,creation_status,completed_status FROM task_settings;
INSERT INTO task_boards SELECT workspace_id,'backlog','Backlog',1,gen_random_uuid(),NULL FROM task_settings;
INSERT INTO task_statuses(id,workspace_id,name,position,board)
 SELECT creation_status,workspace_id,'Backlog',0,'backlog' FROM task_boards WHERE slug='backlog';
ALTER TABLE task_statuses ADD UNIQUE(workspace_id,board,id);
ALTER TABLE task_statuses ADD FOREIGN KEY(workspace_id,board) REFERENCES task_boards(workspace_id,slug) DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE task_boards ADD FOREIGN KEY(workspace_id,slug,creation_status) REFERENCES task_statuses(workspace_id,board,id) DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE task_boards ADD FOREIGN KEY(workspace_id,slug,completed_status) REFERENCES task_statuses(workspace_id,board,id) DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE task_views ADD COLUMN board text NOT NULL DEFAULT 'tasks';
ALTER TABLE task_view_collections ADD COLUMN board text NOT NULL DEFAULT 'tasks';
ALTER TABLE task_views DROP CONSTRAINT task_views_account_id_workspace_id_fkey;
ALTER TABLE task_view_collections DROP CONSTRAINT task_view_collections_pkey;
ALTER TABLE task_view_collections ADD PRIMARY KEY(account_id,workspace_id,board);
ALTER TABLE task_view_collections ADD FOREIGN KEY(workspace_id,board) REFERENCES task_boards(workspace_id,slug);
ALTER TABLE task_views ADD FOREIGN KEY(account_id,workspace_id,board) REFERENCES task_view_collections(account_id,workspace_id,board) ON DELETE CASCADE;
ALTER TABLE task_views DROP CONSTRAINT task_views_account_id_workspace_id_position_key;
ALTER TABLE task_views ADD UNIQUE(account_id,workspace_id,board,position);
DROP INDEX task_view_name;
CREATE UNIQUE INDEX task_view_name ON task_views(account_id,workspace_id,board,lower(name));
ALTER TABLE task_settings DROP COLUMN creation_status, DROP COLUMN completed_status;
CREATE OR REPLACE FUNCTION initialize_workspace_tasks() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE p text; base text; n bigint:=0; suffix text; k bigint; a uuid:=gen_random_uuid(); b uuid:=gen_random_uuid(); c uuid:=gen_random_uuid(); d uuid:=gen_random_uuid();
BEGIN
 base:=left(regexp_replace(upper(NEW.slug),'[^A-Z]','','g'),6);IF length(base)<2 THEN base:='WS'; END IF;p:=base;
 WHILE EXISTS(SELECT 1 FROM task_prefixes WHERE prefix=p) LOOP
 n:=n+1;k:=n;suffix:='';WHILE k>0 LOOP suffix:=chr(65+((k-1)%26)::int)||suffix;k:=(k-1)/26;END LOOP;p:=base||suffix;
 END LOOP;
 INSERT INTO task_boards(workspace_id,slug,name,position,creation_status,completed_status) VALUES(NEW.id,'tasks','Tasks',0,a,c),(NEW.id,'backlog','Backlog',1,d,NULL);
 INSERT INTO task_statuses(id,workspace_id,name,position,board) VALUES(a,NEW.id,'To do',0,'tasks'),(b,NEW.id,'In progress',1,'tasks'),(c,NEW.id,'Done',2,'tasks'),(d,NEW.id,'Backlog',0,'backlog');
 INSERT INTO task_settings(workspace_id,prefix) VALUES(NEW.id,p);
 RETURN NEW;
END; $$;
