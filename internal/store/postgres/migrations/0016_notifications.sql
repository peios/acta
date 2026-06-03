-- 0016_notifications: a per-recipient inbox. Unlike events (the global,
-- append-only activity log), a notification is addressed to ONE recipient and
-- carries read state — "this is for you, and you haven't seen it yet".
--
-- Snapshot columns (actor_name, item_title, workspace_slug, excerpt) are frozen
-- at write time so the bell renders without joins and stays correct after the
-- source rows are renamed or deleted — the same denormalised-history stance as
-- the events table, hence no foreign keys here either.

CREATE TABLE notifications (
    id             text        PRIMARY KEY DEFAULT gen_id(),
    recipient_id   text        NOT NULL,
    kind           text        NOT NULL,
    workspace_id   text        NOT NULL DEFAULT '',
    workspace_slug text        NOT NULL DEFAULT '',
    item_id        text        NOT NULL DEFAULT '',
    item_title     text        NOT NULL DEFAULT '',
    actor_id       text        NOT NULL DEFAULT '',
    actor_name     text        NOT NULL DEFAULT '',
    comment_id     text        NOT NULL DEFAULT '',
    excerpt        text        NOT NULL DEFAULT '',
    created_at     timestamptz NOT NULL DEFAULT now(),
    read_at        timestamptz
);

-- The bell reads a recipient's rows newest-first; the unread badge counts the
-- subset with read_at IS NULL within that same scan.
CREATE INDEX notifications_recipient_idx ON notifications (recipient_id, created_at DESC);
