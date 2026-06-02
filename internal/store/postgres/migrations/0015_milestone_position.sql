-- Milestone columns get their own ordering, independent of each item's lane
-- position (which is shared with regular cards in status mode). ms_position is
-- only meaningful for root milestones; everything else keeps the default 0.
ALTER TABLE items ADD COLUMN ms_position int NOT NULL DEFAULT 0;

-- Backfill a stable per-workspace order matching how milestones are shown today
-- (by lane position, then age), so columns don't reshuffle on the first load
-- after this migration.
WITH ranked AS (
    SELECT id, row_number() OVER (
               PARTITION BY workspace_id
               ORDER BY position, created_at, id) - 1 AS rn
    FROM items
    WHERE is_milestone = true AND parent_id IS NULL AND archived_at IS NULL
)
UPDATE items SET ms_position = ranked.rn
FROM ranked WHERE items.id = ranked.id;
