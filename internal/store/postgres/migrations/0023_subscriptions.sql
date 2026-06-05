-- 0023_subscriptions: generalise notifications beyond @mention. A subscription
-- is a principal's standing interest in a subject — an item, a project, or
-- another principal (their agents). When an event fires, the board fans out to
-- every subscription whose subject matches the event (its item, the item's
-- project, or the actor) and whose events filter includes the event's category,
-- filing an "activity" notification for the subscriber (the actor excluded).

CREATE TABLE subscriptions (
    id            text        PRIMARY KEY DEFAULT gen_id(),
    -- the principal who will be notified. Their subscriptions vanish with them.
    subscriber_id text        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    -- subject_type: item | project | principal. subject_id is the matching id;
    -- it is intentionally not a foreign key — it spans three tables, and a
    -- subscription to a deleted subject is simply inert (it never matches), so
    -- there is nothing to cascade.
    subject_type  text        NOT NULL,
    subject_id    text        NOT NULL,
    -- events is the comma-joined set of category keys the subscriber wants
    -- delivered (comments, status, assignments, items_added, other) — the
    -- configurable filter, seeded from a per-subject-type default. Empty = muted.
    events        text        NOT NULL DEFAULT '',
    created_at    timestamptz NOT NULL DEFAULT now()
);
-- One subscription per (subscriber, subject); the upsert/ensure key.
CREATE UNIQUE INDEX subscriptions_unique_idx ON subscriptions (subscriber_id, subject_type, subject_id);
-- The fanout reads by subject (type, id) to find an event's subscribers.
CREATE INDEX subscriptions_subject_idx ON subscriptions (subject_type, subject_id);

-- Activity notifications carry the event verb and a pre-rendered phrase so the
-- bell renders without re-resolving the event (the same snapshot stance as the
-- rest of the inbox). Both stay '' for the existing mention rows.
ALTER TABLE notifications ADD COLUMN verb    text NOT NULL DEFAULT '';
ALTER TABLE notifications ADD COLUMN summary text NOT NULL DEFAULT '';

-- Backfill the auto-subscribe rule for existing agents: every human is
-- subscribed to their own agents, status-changes only (the principal default).
-- New agents get this at creation; this seeds the ones already present.
INSERT INTO subscriptions (subscriber_id, subject_type, subject_id, events)
SELECT agent_of_id, 'principal', id, 'status'
FROM users
WHERE agent_of_id IS NOT NULL
ON CONFLICT DO NOTHING;
