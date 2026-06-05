-- 0022_projects: a project is a cross-cutting initiative within a workspace — a
-- long-lived area that groups items (e.g. all "Peinit" work) without needing its
-- own workspace. It is orthogonal to boards and the parent/child tree: an item
-- carries an optional project_id regardless of which board, lane, or parent it
-- sits under. This is the foundation the Releases layer will build on.

CREATE TABLE projects (
    id           text        PRIMARY KEY DEFAULT gen_id(),
    workspace_id text        NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    slug         text        NOT NULL,
    name         text        NOT NULL,
    brief        text        NOT NULL DEFAULT '',
    -- lead_id and created_by null out if the user is removed (history, not state).
    lead_id      text        REFERENCES users(id) ON DELETE SET NULL,
    -- status is the project's lifecycle: planned | active | paused | done.
    -- Explicit (a project can be paused with open items) rather than inferred.
    status       text        NOT NULL DEFAULT 'active',
    -- color is '' for an auto palette colour (by position), or an explicit hex.
    color        text        NOT NULL DEFAULT '',
    position     integer     NOT NULL DEFAULT 0,
    archived_at  timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),
    created_by   text        REFERENCES users(id) ON DELETE SET NULL
);
CREATE UNIQUE INDEX projects_workspace_slug_idx ON projects (workspace_id, slug);
CREATE INDEX projects_workspace_idx ON projects (workspace_id, position);

-- An item may belong to one project (its area), or none. Flat — independent of
-- the parent tree and of the item's board/status. ON DELETE SET NULL so removing
-- a project unfiles its items rather than deleting them.
ALTER TABLE items ADD COLUMN project_id text REFERENCES projects(id) ON DELETE SET NULL;
CREATE INDEX items_project_idx ON items (project_id);
