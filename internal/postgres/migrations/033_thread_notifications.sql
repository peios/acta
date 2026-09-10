CREATE SEQUENCE thread_notification_revision;
CREATE TABLE thread_notifications (
 id uuid PRIMARY KEY,
 owner_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
 thread_id uuid NOT NULL REFERENCES provider_threads(id) ON DELETE CASCADE,
 run_id uuid NOT NULL,
 lane_id text NOT NULL DEFAULT '',
 notice_key text NOT NULL,
 turn_id text NOT NULL DEFAULT '',
 kind text NOT NULL,
 title text NOT NULL,
 blocking boolean NOT NULL DEFAULT true,
 source_sequence bigint NOT NULL DEFAULT 0,
 revision bigint NOT NULL DEFAULT nextval('thread_notification_revision'),
 created_at timestamptz NOT NULL DEFAULT now(),
 read_at timestamptz,
 resolved_at timestamptz,
 UNIQUE(thread_id,run_id,lane_id,notice_key)
);
CREATE INDEX thread_notifications_unread ON thread_notifications(owner_id,revision DESC) WHERE read_at IS NULL AND resolved_at IS NULL;
