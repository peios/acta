-- 0014_events: an append-only activity log. One row per meaningful mutation to
-- a board item, attributed to the acting principal.
--
-- actor_name and item_title are snapshotted at write time, and `data` holds
-- small, already-resolved from/to strings (status names, assignee names, old
-- titles). That denormalisation lets the feed render without joins and keeps a
-- line correct even after the item is renamed, reassigned, or hard-deleted — so
-- there are deliberately no foreign keys here: the log is history, not state.

CREATE TABLE events (
    id           text        PRIMARY KEY DEFAULT gen_id(),
    workspace_id text        NOT NULL,
    item_id      text        NOT NULL DEFAULT '',
    item_title   text        NOT NULL DEFAULT '',
    actor_id     text        NOT NULL DEFAULT '',
    actor_name   text        NOT NULL DEFAULT '',
    verb         text        NOT NULL,
    data         jsonb       NOT NULL DEFAULT '{}'::jsonb,
    created_at   timestamptz NOT NULL DEFAULT now()
);

-- Both read paths are newest-first within a scope.
CREATE INDEX events_item_idx      ON events (item_id, created_at DESC);
CREATE INDEX events_workspace_idx ON events (workspace_id, created_at DESC);
