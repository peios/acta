ALTER TABLE activity_entries ADD COLUMN thread_id uuid REFERENCES activity_entries(id);
CREATE INDEX activity_entries_thread ON activity_entries(thread_id,first_event DESC) WHERE thread_id IS NOT NULL;
CREATE TABLE task_comments (
 id uuid PRIMARY KEY REFERENCES activity_entries(id),
 task_id uuid NOT NULL REFERENCES tasks(id),
 author_id uuid NOT NULL REFERENCES accounts(id),
 body text NOT NULL,
 version bigint NOT NULL DEFAULT 1 CHECK(version>0),
 edited boolean NOT NULL DEFAULT false,
 deleted boolean NOT NULL DEFAULT false,
 reply_to uuid REFERENCES task_comments(id),
 request_id uuid NOT NULL,
 request_hash text NOT NULL,
 UNIQUE(author_id,request_id),
 CHECK (NOT deleted OR body='')
);
CREATE INDEX task_comments_task ON task_comments(task_id);
