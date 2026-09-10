CREATE TABLE provider_threads (
 id uuid PRIMARY KEY,
 owner_id uuid NOT NULL REFERENCES accounts(id),
 descriptor jsonb NOT NULL,
 last_sequence bigint NOT NULL DEFAULT 0 CHECK(last_sequence >= 0)
);
CREATE INDEX provider_threads_owner ON provider_threads(owner_id);
CREATE TABLE provider_thread_frames (
 thread_id uuid NOT NULL REFERENCES provider_threads(id),
 sequence bigint NOT NULL CHECK(sequence > 0),
 payload bytea NOT NULL,
 PRIMARY KEY(thread_id,sequence)
);
CREATE TABLE provider_thread_commands (
 id uuid PRIMARY KEY,
 owner_id uuid NOT NULL REFERENCES accounts(id),
 thread_id uuid NOT NULL REFERENCES provider_threads(id),
 action text NOT NULL CHECK(action IN ('kill','resume')),
 outcome jsonb,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX provider_thread_commands_pending ON provider_thread_commands(owner_id,thread_id) WHERE outcome IS NULL;
