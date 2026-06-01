-- 0005_item_detail: deeper items — description, assignee, archive, and comments.
--
-- archived_at (NULL = active) soft-deletes an item: archived items drop off the
-- board but stay restorable from the archive view. assignee_id is an optional
-- single owner. Comments are append-only notes by a user on an item.

ALTER TABLE items
    ADD COLUMN description text NOT NULL DEFAULT '',
    ADD COLUMN assignee_id uuid REFERENCES users(id) ON DELETE SET NULL,
    ADD COLUMN archived_at timestamptz;

-- The board only ever lists active items, so index that path.
CREATE INDEX items_workspace_active_idx ON items (workspace_id) WHERE archived_at IS NULL;

CREATE TABLE comments (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    item_id    uuid        NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    author_id  uuid        REFERENCES users(id) ON DELETE SET NULL,
    body       text        NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX comments_item_idx ON comments (item_id, created_at);
