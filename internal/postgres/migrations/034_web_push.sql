CREATE TABLE push_subscriptions (
 id uuid PRIMARY KEY,
 owner_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
 session_id uuid NOT NULL REFERENCES browser_sessions(id) ON DELETE CASCADE,
 endpoint text NOT NULL UNIQUE,
 p256dh text NOT NULL,
 auth text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX push_subscriptions_owner ON push_subscriptions(owner_id);
CREATE TABLE push_deliveries (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 subscription_id uuid NOT NULL REFERENCES push_subscriptions(id) ON DELETE CASCADE,
 notification_id uuid NOT NULL REFERENCES thread_notifications(id) ON DELETE CASCADE,
 revision bigint NOT NULL,
 attempts integer NOT NULL DEFAULT 0,
 due_at timestamptz NOT NULL DEFAULT now()+interval '3 seconds',
 expires_at timestamptz NOT NULL DEFAULT now()+interval '24 hours',
 lease uuid,
 done boolean NOT NULL DEFAULT false,
 UNIQUE(subscription_id,notification_id,revision)
);
CREATE INDEX push_deliveries_due ON push_deliveries(due_at) WHERE NOT done;
