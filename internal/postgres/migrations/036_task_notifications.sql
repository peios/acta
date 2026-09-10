ALTER TABLE thread_notifications RENAME TO notifications;
ALTER TABLE notifications ALTER COLUMN thread_id DROP NOT NULL;
ALTER TABLE notifications ALTER COLUMN run_id DROP NOT NULL;
ALTER TABLE notifications ADD COLUMN task_id uuid REFERENCES tasks(id) ON DELETE CASCADE;
ALTER TABLE notifications ADD COLUMN activity_id uuid REFERENCES activity_entries(id) ON DELETE CASCADE;
ALTER TABLE notifications ADD CONSTRAINT notification_subject CHECK ((thread_id IS NOT NULL) <> (task_id IS NOT NULL));
CREATE UNIQUE INDEX notifications_task_entry ON notifications(owner_id,task_id,activity_id) WHERE task_id IS NOT NULL;
CREATE TABLE task_followers (
 task_id uuid NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
 owner_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
 following boolean NOT NULL,
 PRIMARY KEY(task_id,owner_id)
);
-- Seed following, never historical notifications, for work already in use.
INSERT INTO task_followers(task_id,owner_id,following)
SELECT t.id,COALESCE(a.parent_id,a.id),true FROM tasks t JOIN accounts a ON a.id=t.created_by
UNION SELECT s.task_id,COALESCE(a.parent_id,a.id),true FROM task_assignees s JOIN accounts a ON a.id=s.account_id
ON CONFLICT DO NOTHING;
