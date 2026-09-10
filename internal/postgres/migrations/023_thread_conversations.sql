ALTER TABLE provider_threads ADD COLUMN conversation_version integer NOT NULL DEFAULT 0;
ALTER TABLE provider_threads ADD COLUMN conversation_state jsonb NOT NULL DEFAULT '{}';
CREATE TABLE thread_conversation_items (
 thread_id uuid NOT NULL REFERENCES provider_threads(id),
 id text NOT NULL,
 position_sequence bigint NOT NULL,
 position_output integer NOT NULL,
 revision bigint NOT NULL,
 run_id uuid NOT NULL,
 turn_id text NOT NULL DEFAULT '',
 visibility text NOT NULL CHECK(visibility IN ('normal','debug','hidden')),
 deleted boolean NOT NULL DEFAULT false,
 payload jsonb NOT NULL,
 internal jsonb NOT NULL DEFAULT '{}',
 PRIMARY KEY(thread_id,id)
);
CREATE INDEX thread_conversation_position ON thread_conversation_items(thread_id,position_sequence DESC,position_output DESC,id DESC) WHERE NOT deleted AND visibility='normal';
CREATE INDEX thread_conversation_debug_position ON thread_conversation_items(thread_id,position_sequence DESC,position_output DESC,id DESC) WHERE NOT deleted AND visibility IN ('normal','debug');
CREATE INDEX thread_conversation_changes ON thread_conversation_items(thread_id,revision);
CREATE INDEX thread_conversation_turn ON thread_conversation_items(thread_id,run_id,turn_id);
