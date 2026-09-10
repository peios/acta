CREATE TABLE workspaces (
 id uuid PRIMARY KEY,
 name text NOT NULL,
 slug text COLLATE "C" NOT NULL UNIQUE,
 description text NOT NULL DEFAULT '',
 version bigint NOT NULL DEFAULT 1,
 created_at timestamptz NOT NULL DEFAULT now(),
 CHECK(slug ~ '^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$')
);
CREATE TABLE workspace_slugs (
 slug text COLLATE "C" PRIMARY KEY,
 workspace_id uuid NOT NULL REFERENCES workspaces(id)
);
CREATE INDEX workspace_slug_owner ON workspace_slugs(workspace_id);
CREATE FUNCTION claim_workspace_slug() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO workspace_slugs(slug,workspace_id) VALUES(NEW.slug,NEW.id)
 ON CONFLICT(slug) DO UPDATE SET workspace_id=EXCLUDED.workspace_id WHERE workspace_slugs.workspace_id=EXCLUDED.workspace_id;
 IF NOT FOUND THEN RAISE EXCEPTION 'slug is reserved' USING ERRCODE='23505',CONSTRAINT='workspace_slugs_pkey'; END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER workspace_slug_claim AFTER INSERT OR UPDATE OF slug ON workspaces FOR EACH ROW EXECUTE FUNCTION claim_workspace_slug();
CREATE TABLE workspace_members (
 workspace_id uuid NOT NULL REFERENCES workspaces(id),
 account_id uuid NOT NULL REFERENCES accounts(id),
 permissions text[] NOT NULL DEFAULT '{}',
 PRIMARY KEY(workspace_id,account_id)
);
CREATE INDEX workspace_member_account ON workspace_members(account_id,workspace_id);
CREATE TRIGGER workspace_member_human BEFORE INSERT OR UPDATE ON workspace_members FOR EACH ROW EXECUTE FUNCTION require_human_account();
CREATE TABLE workspace_group_grants (
 workspace_id uuid NOT NULL REFERENCES workspaces(id),
 group_id uuid NOT NULL REFERENCES permission_groups(id) ON DELETE CASCADE,
 permissions text[] NOT NULL DEFAULT '{}',
 PRIMARY KEY(workspace_id,group_id)
);
CREATE INDEX workspace_group_source ON workspace_group_grants(group_id);
CREATE TABLE workspace_visits (
 account_id uuid NOT NULL REFERENCES accounts(id),
 workspace_id uuid NOT NULL REFERENCES workspaces(id),
 visited_at timestamptz NOT NULL,
 PRIMARY KEY(account_id,workspace_id)
);
CREATE TABLE agent_workspace_access (
 account_id uuid PRIMARY KEY REFERENCES accounts(id),
 all_workspaces boolean NOT NULL DEFAULT true,
 version bigint NOT NULL DEFAULT 1
);
CREATE TABLE agent_workspace_policies (
 account_id uuid NOT NULL REFERENCES accounts(id),
 workspace_id uuid NOT NULL REFERENCES workspaces(id),
 selected boolean NOT NULL DEFAULT false,
 inherit boolean NOT NULL DEFAULT true,
 permissions text[] NOT NULL DEFAULT '{}',
 PRIMARY KEY(account_id,workspace_id)
);
CREATE INDEX agent_workspace_scope ON agent_workspace_policies(workspace_id);
CREATE FUNCTION require_agent_account() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM accounts WHERE id=NEW.account_id AND parent_id IS NOT NULL) THEN
 RAISE EXCEPTION 'agent policy requires an agent' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER agent_access_identity BEFORE INSERT OR UPDATE ON agent_workspace_access FOR EACH ROW EXECUTE FUNCTION require_agent_account();
CREATE TRIGGER agent_policy_identity BEFORE INSERT OR UPDATE ON agent_workspace_policies FOR EACH ROW EXECUTE FUNCTION require_agent_account();
CREATE TABLE workspace_events (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 workspace_id uuid NOT NULL REFERENCES workspaces(id),
 actor_id uuid NOT NULL REFERENCES accounts(id),
 kind text NOT NULL,
 occurred_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX workspace_event_history ON workspace_events(workspace_id,id);

ALTER TABLE accounts ADD CONSTRAINT agent_no_workspace_creation CHECK(parent_id IS NULL OR NOT ('site.workspaces.create'=ANY(direct_permissions)));
