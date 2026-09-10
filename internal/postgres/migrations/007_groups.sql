CREATE TABLE permission_groups (
 id uuid PRIMARY KEY,
 name text NOT NULL CHECK(length(name)>0),
 description text NOT NULL DEFAULT '',
 is_default boolean NOT NULL DEFAULT false,
 permissions text[] NOT NULL DEFAULT '{}',
 require_mfa boolean NOT NULL DEFAULT false,
 version bigint NOT NULL DEFAULT 1,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX group_name_unique ON permission_groups(lower(name));
CREATE UNIQUE INDEX one_default_group ON permission_groups(is_default) WHERE is_default;
INSERT INTO permission_groups(id,name,is_default,permissions) VALUES
 ('00000000-0000-4000-8000-000000000001','Default',true,ARRAY['own.username','own.display_name']);
CREATE TABLE group_memberships (
 group_id uuid NOT NULL REFERENCES permission_groups(id) ON DELETE CASCADE,
 account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
 PRIMARY KEY(group_id,account_id)
);
CREATE INDEX account_group_memberships ON group_memberships(account_id,group_id);
CREATE TABLE group_events (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 group_id uuid NOT NULL REFERENCES permission_groups(id),
 actor_id uuid NOT NULL REFERENCES accounts(id),
 account_id uuid REFERENCES accounts(id),
 kind text NOT NULL,
 occurred_at timestamptz NOT NULL
);
-- This view serves the transactional last-Superuser invariant, not feature
-- authorization. Group and direct Superuser sources count once per account.
CREATE VIEW superuser_accounts AS
 SELECT id FROM accounts WHERE 'site.superuser'=ANY(direct_permissions)
 UNION SELECT m.account_id FROM group_memberships m JOIN permission_groups g ON g.id=m.group_id WHERE 'site.superuser'=ANY(g.permissions);
ALTER TABLE accounts ALTER COLUMN direct_permissions SET DEFAULT '{}';
