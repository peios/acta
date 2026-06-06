-- Item attributes: priority, type and size are small fixed enums stored as ints
-- (0 = unset), and due_date is an optional calendar date. All four are scalar
-- columns on items — the cheapest field shape, like assignee_id — rather than
-- joins, since each item has at most one of each. Every column is optional and
-- defaults to unset, so existing items are untouched by the backfill.
ALTER TABLE items
    ADD COLUMN priority  integer NOT NULL DEFAULT 0,  -- 0 none, 1 low, 2 medium, 3 high, 4 urgent
    ADD COLUMN item_type integer NOT NULL DEFAULT 0,  -- 0 none, 1 feature, 2 bug, 3 chore
    ADD COLUMN size      integer NOT NULL DEFAULT 0,  -- 0 none, 1 XS, 2 S, 3 M, 4 L, 5 XL
    ADD COLUMN due_date  date;                        -- null = no due date
