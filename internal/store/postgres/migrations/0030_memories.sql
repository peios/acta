-- 0030_memories: arbitrary markdown "memories" accumulated under a scope. Stored
-- inline (not as files) so they sync across machines and harnesses. The owner is
-- polymorphic: scope names the kind of thing the memory belongs to ("agent" for
-- this first slice; "user"/"site"/"workspace"/"project"/"task" later) and
-- scope_id is that thing's id — a users.id for agent/user scope, "" for the
-- single site scope. There's no foreign key because scope_id points at different
-- tables per scope; cleaning up an owner's memories is handled in application
-- code. name is a short label/filename; body is markdown, rendered through the
-- same pipeline as descriptions, comments, and documents.
CREATE TABLE memories (
    id         text        PRIMARY KEY DEFAULT gen_id(),
    scope      text        NOT NULL,
    scope_id   text        NOT NULL DEFAULT '',
    name       text        NOT NULL,
    body       text        NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX memories_scope_idx ON memories (scope, scope_id, name);
