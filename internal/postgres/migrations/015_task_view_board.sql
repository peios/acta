UPDATE task_views SET display = display || '{"mode":"table"}'::jsonb;
ALTER TABLE task_views ALTER COLUMN display SET DEFAULT
 '{"mode":"table","columns":["status","assignees"],"sort":"number","direction":"desc","group":"none","density":"comfortable"}';
