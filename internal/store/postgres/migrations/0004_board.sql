-- 0004_board: the board — user-defined statuses (the lanes) and items.
--
-- A status is a named, ordered lane within one workspace; users create, rename,
-- reorder, and delete them (deletion is blocked by the app while a lane still
-- holds items). An item has a title and lives in exactly one status, ordered by
-- position within that lane. Moving an item between lanes is a transition.
--
-- position is a plain 0..n-1 integer kept dense by the app on every reorder, so
-- "ORDER BY position" is the lane order. Both tables cascade from the workspace.

CREATE TABLE statuses (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid        NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name         text        NOT NULL,
    position     integer     NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX statuses_workspace_idx ON statuses (workspace_id, position);

CREATE TABLE items (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid        NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    status_id    uuid        NOT NULL REFERENCES statuses(id) ON DELETE CASCADE,
    title        text        NOT NULL,
    position     integer     NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX items_status_idx ON items (status_id, position);

-- Seed the default lanes for the already-seeded General workspace so its board
-- is usable straight away. New workspaces get the same set from the app layer.
INSERT INTO statuses (workspace_id, name, position)
SELECT w.id, v.name, v.position
FROM workspaces w
CROSS JOIN (VALUES ('To do', 0), ('Doing', 1), ('Done', 2)) AS v(name, position)
WHERE w.slug = 'general';
