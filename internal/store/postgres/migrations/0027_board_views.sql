-- 0027_board_views: saved, named filters on a board ("views").
--
-- A board's header tabs (All items / My items / Current Release / Milestones /
-- Releases) used to be hardcoded in the template. They become rows here so they
-- can be renamed, reordered, deleted, and joined by user-created views. A view
-- is just a name + an icon + a query string (the filter-defining URL params:
-- mode, status[], assignee[], project[], release[], q) scoped to one board;
-- clicking it navigates to the board with that query. No new render path.
--
-- query is stored already-normalised by the app (whitelisted + canonical order),
-- so "" is the All-items view and the active tab is the one whose query equals
-- the current normalised query. created_by is null for the seeded defaults.

CREATE TABLE board_views (
    id           text        PRIMARY KEY DEFAULT gen_id(),
    workspace_id text        NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    board_id     text        NOT NULL REFERENCES boards(id) ON DELETE CASCADE,
    slug         text        NOT NULL,
    name         text        NOT NULL,
    icon         text        NOT NULL DEFAULT 'filter',
    query        text        NOT NULL DEFAULT '',
    position     integer     NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    created_by   text        REFERENCES users(id) ON DELETE SET NULL
);
CREATE UNIQUE INDEX board_views_board_slug_idx ON board_views (board_id, slug);
CREATE INDEX board_views_board_idx ON board_views (board_id, position);

-- Backfill the five defaults onto every existing board (mirrors seedBoardViews
-- in the board service, which does the same for boards created later). Keep the
-- slug/name/icon/query/position in sync with DefaultBoardViews there.
INSERT INTO board_views (workspace_id, board_id, slug, name, icon, query, position)
SELECT b.workspace_id, b.id, v.slug, v.name, v.icon, v.query, v.position
FROM boards b
CROSS JOIN (VALUES
    ('all-items',       'All items',       'columns', '',               0),
    ('my-items',        'My items',        'person',  'assignee=me',    1),
    ('current-release', 'Current Release', 'hexagon', 'release=active', 2),
    ('milestones',      'Milestones',      'diamond', 'mode=milestone', 3),
    ('releases',        'Releases',        'cube',    'mode=release',   4)
) AS v(slug, name, icon, query, position);
