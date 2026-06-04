-- 0020_backlog_board: every workspace gains its second board — "Backlog" — a
-- feature-equivalent peer of Tasks for unscheduled work. It seeds with a single
-- "Backlog" lane (the entry lane); most workspaces keep just that one. An item
-- joins the Backlog board simply by taking a Backlog-board status, so no item
-- data moves here — only the board and its lane are created.
--
-- The NOT EXISTS guards keep this idempotent against a workspace that somehow
-- already has a backlog board (e.g. created by the updated seed path).

INSERT INTO boards (workspace_id, name, slug, position)
SELECT w.id, 'Backlog', 'backlog', 1
FROM workspaces w
WHERE NOT EXISTS (
    SELECT 1 FROM boards b WHERE b.workspace_id = w.id AND b.slug = 'backlog'
);

INSERT INTO statuses (workspace_id, board_id, name, position, color, is_entry)
SELECT b.workspace_id, b.id, 'Backlog', 0, '', true
FROM boards b
WHERE b.slug = 'backlog'
  AND NOT EXISTS (SELECT 1 FROM statuses s WHERE s.board_id = b.id);
