-- Delivery confirmation must not depend on which history page is loaded.
CREATE INDEX thread_conversation_submission ON thread_conversation_items
 (thread_id, (payload #>> '{frame,data,submission_id}'))
 WHERE payload->>'kind'='user-message';
