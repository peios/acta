-- Web Push subscriptions: one row per browser/device a user has opted in from.
-- The push-service endpoint is the natural key (globally unique per browser),
-- so re-subscribing the same browser upserts on it. Deleting the user drops
-- their subscriptions with them.
CREATE TABLE push_subscriptions (
    endpoint   text PRIMARY KEY,
    user_id    text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    p256dh     text NOT NULL,
    auth       text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- Sends fan out by user, so index the lookup.
CREATE INDEX push_subscriptions_user_idx ON push_subscriptions (user_id);
