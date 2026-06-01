-- 0006_subtasks: items can nest under a parent item, to any depth.
--
-- parent_id NULL = a top-level item (a board card). A non-NULL parent_id makes
-- the item a subtask of that parent; it stays a full item (own status,
-- assignee, description, comments) but lives in the parent's modal rather than
-- on the board. position then orders it within its parent instead of a lane.
-- ON DELETE CASCADE means deleting an item removes its whole subtree.

ALTER TABLE items
    ADD COLUMN parent_id uuid REFERENCES items(id) ON DELETE CASCADE;

CREATE INDEX items_parent_idx ON items (parent_id) WHERE parent_id IS NOT NULL;
