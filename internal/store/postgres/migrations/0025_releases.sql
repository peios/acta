-- Releases: a workspace's versioned cut-lines. Unlike a project (an open-ended
-- theme an item belongs to indefinitely), a release freezes at a moment — it
-- accrues items while "active", then "shipped" stamps shipped_at and it becomes
-- an immutable changelog entry. Several releases can be active at once.
CREATE TABLE releases (
    id           text        PRIMARY KEY DEFAULT gen_id(),
    workspace_id text        NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name         text        NOT NULL,
    description  text        NOT NULL DEFAULT '',
    status       text        NOT NULL DEFAULT 'active', -- active|shipped
    shipped_at   timestamptz,                           -- null until shipped (the freeze marker)
    position     integer     NOT NULL DEFAULT 0,
    created_at   timestamptz NOT NULL DEFAULT now(),
    created_by   text        REFERENCES users(id) ON DELETE SET NULL
);
-- The name is the version handle (e.g. "v0.27.0"), unique within a workspace
-- case-insensitively — the same rule a fact's title uses.
CREATE UNIQUE INDEX releases_workspace_name_idx ON releases (workspace_id, lower(name));
CREATE INDEX releases_workspace_idx ON releases (workspace_id, position);

-- Many-to-many item↔release membership. Each row is one membership; both sides
-- cascade, so deleting a release or an item drops its links without orphans. The
-- join is deliberately many-to-many — a backported fix can ship in more than one
-- release — even though the UI enforces one release per item for now.
CREATE TABLE item_releases (
    item_id    text        NOT NULL REFERENCES items(id)    ON DELETE CASCADE,
    release_id text        NOT NULL REFERENCES releases(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (item_id, release_id)
);
CREATE INDEX item_releases_release_idx ON item_releases (release_id);
