-- Retire built-in execution without changing checksums of applied migrations.
-- Task notifications and their queued deliveries remain intact.
DELETE FROM notifications WHERE thread_id IS NOT NULL;
ALTER TABLE notifications DROP CONSTRAINT notification_subject;
ALTER TABLE notifications ALTER COLUMN task_id SET NOT NULL;
ALTER TABLE notifications DROP COLUMN thread_id, DROP COLUMN run_id,
 DROP COLUMN lane_id, DROP COLUMN turn_id, DROP COLUMN blocking,
 DROP COLUMN notice_key;
ALTER SEQUENCE thread_notification_revision RENAME TO notification_revision;
ALTER INDEX thread_notifications_unread RENAME TO notifications_unread;
DROP TABLE thread_conversation_items, provider_thread_commands,
 provider_thread_frames, provider_threads, code_hosts;
