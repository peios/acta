-- Raw records are append-only. Entries are a rebuildable presentation projection.
CREATE TABLE activity_events (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 entry_id uuid NOT NULL,
 workspace_id uuid NOT NULL REFERENCES workspaces(id),
 subject_type text NOT NULL,
 subject_id uuid NOT NULL,
 actor_id uuid NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
 schema_version integer NOT NULL DEFAULT 1,
 occurred_at timestamptz NOT NULL,
 change jsonb NOT NULL
);
CREATE INDEX activity_events_subject ON activity_events(subject_type,subject_id,id);
CREATE INDEX activity_events_workspace ON activity_events(workspace_id,id);
CREATE INDEX activity_events_entry ON activity_events(entry_id,id);
CREATE TABLE activity_entries (
 id uuid PRIMARY KEY,
 workspace_id uuid NOT NULL REFERENCES workspaces(id),
 subject_type text NOT NULL,
 subject_id uuid NOT NULL,
 actor_id uuid NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
 first_event bigint NOT NULL REFERENCES activity_events(id),
 last_event bigint NOT NULL REFERENCES activity_events(id),
 started_at timestamptz NOT NULL,
 updated_at timestamptz NOT NULL,
 event_count integer NOT NULL CHECK(event_count>0),
 change jsonb NOT NULL
);
CREATE INDEX activity_entries_subject ON activity_entries(subject_type,subject_id,first_event DESC);
CREATE INDEX activity_entries_workspace ON activity_entries(workspace_id,first_event DESC);
-- Acknowledgements are sparse so seeing new entries never marks unseen older ones.
CREATE TABLE activity_reads (
 account_id uuid NOT NULL REFERENCES accounts(id),
 entry_id uuid NOT NULL REFERENCES activity_entries(id),
 through_event bigint NOT NULL REFERENCES activity_events(id),
 PRIMARY KEY(account_id,entry_id)
);
CREATE FUNCTION preserve_activity_events() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'activity events are append-only'; END; $$;
CREATE TRIGGER activity_events_immutable BEFORE UPDATE OR DELETE ON activity_events
 FOR EACH ROW EXECUTE FUNCTION preserve_activity_events();
