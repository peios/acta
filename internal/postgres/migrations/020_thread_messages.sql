ALTER TABLE provider_thread_commands DROP CONSTRAINT provider_thread_commands_action_check;
ALTER TABLE provider_thread_commands ADD CONSTRAINT provider_thread_commands_action_check CHECK(action IN ('kill','resume','send'));
ALTER TABLE provider_thread_commands ADD COLUMN run_id text NOT NULL DEFAULT '';
ALTER TABLE provider_thread_commands ADD COLUMN message_text text NOT NULL DEFAULT '';
