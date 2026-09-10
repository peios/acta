ALTER TABLE provider_thread_commands DROP CONSTRAINT provider_thread_commands_action_check;
ALTER TABLE provider_thread_commands ADD CONSTRAINT provider_thread_commands_action_check
    CHECK (action IN ('kill', 'resume', 'send', 'models', 'configure'));

-- Keep immutable command input together as controls gain typed payloads.
ALTER TABLE provider_thread_commands ADD COLUMN payload jsonb;
UPDATE provider_thread_commands SET payload = jsonb_strip_nulls(jsonb_build_object(
    'id', id, 'thread_id', thread_id, 'action', action,
    'run_id', NULLIF(run_id,''), 'text', NULLIF(message_text,'')));
ALTER TABLE provider_thread_commands ALTER COLUMN payload SET NOT NULL;
ALTER TABLE provider_thread_commands DROP COLUMN run_id, DROP COLUMN message_text;
