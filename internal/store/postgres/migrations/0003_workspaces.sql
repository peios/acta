-- 0003_workspaces: top-level work containers.
--
-- Workspaces are shared/global for now: every signed-in user sees and can
-- switch to all of them. created_by records who made it (nullable — the
-- seeded default has none, and we keep a workspace if its creator is removed),
-- but no membership/ACL is enforced yet; that's a future slice.
--
-- slug is the immutable, URL-safe identifier used in /w/{slug}/… paths, so a
-- rename (which only touches name) never breaks a bookmarked link. name is the
-- human label, unique case-insensitively.

CREATE TABLE workspaces (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    slug       text        NOT NULL UNIQUE,
    name       text        NOT NULL,
    created_by uuid        REFERENCES users(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX workspaces_name_lower_idx ON workspaces (lower(name));

-- Seed a default so the workspace switcher is never empty and / always has
-- somewhere to redirect to. The last workspace can't be deleted, so there is
-- always at least one from here on.
INSERT INTO workspaces (slug, name) VALUES ('general', 'General');
