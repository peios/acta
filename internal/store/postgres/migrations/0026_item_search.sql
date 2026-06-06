-- Full-text-ish search behind list_items' `q` parameter is a case-insensitive
-- substring match (title/description ILIKE) — the right semantics for a tracker
-- full of identifiers and ref-ids, where stemming/tokenisation would hurt. These
-- GIN trigram indexes let the planner serve `ILIKE '%term%'` without a seq scan
-- once the corpus grows; they change nothing about the matching itself.
--
-- pg_trgm is a trusted extension on PG13+, so a database owner can create it.
-- Plain CREATE INDEX (not CONCURRENTLY) because the migration runner wraps each
-- file in a single transaction.
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE INDEX IF NOT EXISTS items_title_trgm
    ON items USING gin (title gin_trgm_ops);

CREATE INDEX IF NOT EXISTS items_description_trgm
    ON items USING gin (description gin_trgm_ops);
