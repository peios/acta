ALTER TABLE provider_thread_commands DROP CONSTRAINT provider_thread_commands_action_check;
ALTER TABLE provider_thread_commands ADD CONSTRAINT provider_thread_commands_action_check
 CHECK (action IN ('kill','resume','send','models','configure','permissions','approval','interrupt','answer'));
