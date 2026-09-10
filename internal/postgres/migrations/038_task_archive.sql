ALTER TABLE tasks ADD COLUMN archived_at timestamptz;
ALTER TABLE tasks ADD COLUMN archive_batch uuid;
ALTER TABLE tasks ADD COLUMN archive_version bigint NOT NULL DEFAULT 1;
ALTER TABLE tasks ADD CONSTRAINT task_archive_pair CHECK ((archived_at IS NULL) = (archive_batch IS NULL));
CREATE INDEX tasks_archive_listing ON tasks(workspace_id, (archived_at IS NOT NULL), parent_id, number);
