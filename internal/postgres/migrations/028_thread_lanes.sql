ALTER TABLE thread_conversation_items ADD COLUMN lane_id text NOT NULL DEFAULT '';
CREATE INDEX thread_conversation_lane_position ON thread_conversation_items(thread_id,lane_id,position_sequence DESC,position_output DESC,id DESC) WHERE NOT deleted;
CREATE INDEX thread_conversation_lane_revision ON thread_conversation_items(thread_id,lane_id,revision,id);
