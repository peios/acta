CREATE TABLE memories (
 id uuid PRIMARY KEY,
 scope text NOT NULL CHECK(scope IN ('site','workspace','user','agent')),
 workspace_id uuid REFERENCES workspaces(id),
 account_id uuid REFERENCES accounts(id),
 key text NOT NULL,
 summary text NOT NULL,
 content text NOT NULL,
 revision bigint NOT NULL DEFAULT 1 CHECK(revision>0),
 created_by uuid NOT NULL REFERENCES accounts(id),
 updated_by uuid NOT NULL REFERENCES accounts(id),
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 CHECK ((scope='site' AND workspace_id IS NULL AND account_id IS NULL) OR (scope='workspace' AND workspace_id IS NOT NULL AND account_id IS NULL) OR (scope IN ('user','agent') AND account_id IS NOT NULL AND workspace_id IS NULL))
);
CREATE UNIQUE INDEX memory_scope_key ON memories(scope,COALESCE(workspace_id,account_id,'00000000-0000-0000-0000-000000000000'::uuid),key);
CREATE INDEX memory_listing ON memories(key,id);
