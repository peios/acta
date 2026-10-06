-- Releases are workspace-scoped and never deleted, so a task's target cannot dangle.
CREATE TABLE task_releases (
 id uuid PRIMARY KEY,
 workspace_id uuid NOT NULL REFERENCES workspaces(id),
 name text NOT NULL,
 codename text NOT NULL DEFAULT '',
 state text NOT NULL DEFAULT 'planned' CHECK (state IN ('planned','open','frozen','released')),
 description text NOT NULL DEFAULT '',
 version bigint NOT NULL DEFAULT 1,
 created_by uuid NOT NULL REFERENCES accounts(id),
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(workspace_id,id)
);
CREATE UNIQUE INDEX task_release_name ON task_releases(workspace_id,lower(name));
-- The composite key keeps a task's release inside the task's own workspace.
ALTER TABLE tasks
 ADD COLUMN release_id uuid,
 ADD COLUMN release_version bigint NOT NULL DEFAULT 1,
 ADD FOREIGN KEY(workspace_id,release_id) REFERENCES task_releases(workspace_id,id);
CREATE INDEX task_release ON tasks(release_id) WHERE release_id IS NOT NULL;
