-- 0019_boards: a workspace can hold more than one board. v1 ships exactly two —
-- Tasks (the existing board) and, from the next slice, Backlog — but the model
-- is a first-class table so the count isn't baked in. A board groups statuses
-- (lanes); an item's board is derived from its status's board and is never
-- stored on the item, so moving an item to a status on another board *is* its
-- board move (nothing to keep in sync).
--
-- This slice only introduces the table and retrofits every existing lane onto a
-- per-workspace "Tasks" board — behaviour is unchanged (still one board). The
-- Backlog board and the UI to reach it come in the next slice.

CREATE TABLE boards (
    id           text        PRIMARY KEY DEFAULT gen_id(),
    workspace_id text        NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name         text        NOT NULL,
    slug         text        NOT NULL,
    position     integer     NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX boards_workspace_slug_idx ON boards (workspace_id, slug);
CREATE INDEX boards_workspace_idx ON boards (workspace_id, position);

-- One Tasks board per existing workspace; all of its current lanes belong to it.
INSERT INTO boards (workspace_id, name, slug, position)
SELECT id, 'Tasks', 'tasks', 0 FROM workspaces;

-- Lanes belong to a board. Backfill every existing lane onto its workspace's
-- Tasks board, then pin the column NOT NULL.
ALTER TABLE statuses ADD COLUMN board_id text REFERENCES boards(id) ON DELETE CASCADE;
UPDATE statuses s SET board_id = b.id
    FROM boards b WHERE b.workspace_id = s.workspace_id AND b.slug = 'tasks';
ALTER TABLE statuses ALTER COLUMN board_id SET NOT NULL;
CREATE INDEX statuses_board_idx ON statuses (board_id, position);

-- The entry lane: where new (and cross-board) items land. Exactly one per board
-- — the DB enforces "no two" with a partial unique index, the app enforces "at
-- least one". Seed it as each board's lowest-position lane.
ALTER TABLE statuses ADD COLUMN is_entry boolean NOT NULL DEFAULT false;
UPDATE statuses SET is_entry = true WHERE id IN (
    SELECT DISTINCT ON (board_id) id FROM statuses
    ORDER BY board_id, position, created_at, id
);
CREATE UNIQUE INDEX statuses_one_entry_per_board ON statuses (board_id) WHERE is_entry;

-- The activity log records which board an event happened on, so each board has
-- its own feed. No foreign key (the log is history, not state, like the rest of
-- this table); every existing event belongs to its workspace's Tasks board.
ALTER TABLE events ADD COLUMN board_id text NOT NULL DEFAULT '';
UPDATE events e SET board_id = b.id
    FROM boards b WHERE b.workspace_id = e.workspace_id AND b.slug = 'tasks';
CREATE INDEX events_board_idx ON events (board_id, created_at DESC);
