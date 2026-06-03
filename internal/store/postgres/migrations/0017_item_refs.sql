-- Human-readable, workspace-scoped item ids: PREFIX-N (e.g. ACTA-12).
-- The opaque items.id stays the stable primary key. ref_num is an immutable
-- per-workspace sequence number; item_prefix is an editable, globally-unique
-- label. The display id (prefix || '-' || ref_num) is computed at render, so
-- changing a prefix re-labels every item with no data migration.

ALTER TABLE workspaces ADD COLUMN item_prefix text NOT NULL DEFAULT '';
-- Monotonic per-workspace counter; only ever incremented, so a number is never
-- reused even after an item is hard-deleted.
ALTER TABLE workspaces ADD COLUMN item_seq integer NOT NULL DEFAULT 0;
ALTER TABLE items ADD COLUMN ref_num integer;

-- Backfill ref_num: number existing items per workspace by creation order.
WITH numbered AS (
    SELECT id, row_number() OVER (PARTITION BY workspace_id ORDER BY created_at, id) AS rn
    FROM items
)
UPDATE items SET ref_num = numbered.rn FROM numbered WHERE items.id = numbered.id;

-- Each workspace's counter starts at its current highest number.
UPDATE workspaces w
SET item_seq = COALESCE((SELECT max(ref_num) FROM items WHERE workspace_id = w.id), 0);

-- Default prefix for existing workspaces: first three alphanumerics of the name,
-- uppercased, de-duplicated with a numeric suffix so the unique index holds.
WITH base AS (
    SELECT id,
           upper(substr(regexp_replace(name, '[^a-zA-Z0-9]', '', 'g'), 1, 3)) AS p,
           row_number() OVER (
               PARTITION BY upper(substr(regexp_replace(name, '[^a-zA-Z0-9]', '', 'g'), 1, 3))
               ORDER BY created_at, id) AS rn
    FROM workspaces
)
UPDATE workspaces w
SET item_prefix = CASE WHEN base.rn = 1 THEN base.p ELSE base.p || base.rn::text END
FROM base WHERE w.id = base.id AND base.p <> '';

ALTER TABLE items ALTER COLUMN ref_num SET NOT NULL;
CREATE UNIQUE INDEX items_workspace_ref_idx ON items (workspace_id, ref_num);
CREATE UNIQUE INDEX workspaces_prefix_lower_idx ON workspaces (lower(item_prefix)) WHERE item_prefix <> '';
