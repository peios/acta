ALTER TABLE task_views ADD COLUMN display jsonb NOT NULL DEFAULT
 '{"columns":["status","assignees"],"sort":"number","direction":"desc","group":"none","density":"comfortable"}'
 CHECK(jsonb_typeof(display)='object');
CREATE INDEX tasks_workspace_title_sort ON tasks(workspace_id,lower(title) COLLATE "C",number);
CREATE INDEX tasks_workspace_created_sort ON tasks(workspace_id,created_at,number);
CREATE INDEX tasks_workspace_updated_sort ON tasks(workspace_id,updated_at,number);
