-- 0024_status_checklists: gate a status behind a checklist of facts. A *fact* is
-- a truth about an item ("Provium tests pass"), defined once per workspace. A
-- status declares which facts gate entry into it; an item carries a fact as
-- ticked or not, and that tick satisfies every gate that requires the fact — a
-- fact is a property of the item, not of a single transition. An item may only
-- enter a gated status once all the lane's facts are ticked (or it is forced),
-- so until then it sits in its current lane with a pending transition recorded.

-- The workspace's fact vocabulary. Facts are identified by their title (their
-- real name); the integer id is an internal handle so renames don't break the
-- join rows that reference them. No gen_id() text handle — a fact never appears
-- in a URL.
CREATE TABLE checklist_facts (
    id           bigserial   PRIMARY KEY,
    workspace_id text        NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    title        text        NOT NULL,
    position     integer     NOT NULL DEFAULT 0,
    created_at   timestamptz NOT NULL DEFAULT now()
);
-- One fact per title within a workspace (the vocabulary is a set of names).
CREATE UNIQUE INDEX checklist_facts_title_idx ON checklist_facts (workspace_id, lower(title));

-- Which facts gate which status. Ordered, so a lane's checklist has a stable
-- reading order. Both sides cascade: drop a status or a fact and its gates go.
CREATE TABLE status_facts (
    status_id text    NOT NULL REFERENCES statuses(id) ON DELETE CASCADE,
    fact_id   bigint  NOT NULL REFERENCES checklist_facts(id) ON DELETE CASCADE,
    position  integer NOT NULL DEFAULT 0,
    PRIMARY KEY (status_id, fact_id)
);

-- A ticked fact on an item. The row's existence *is* the tick; unticking
-- deletes it. checked_by/checked_at are the audit trail (who asserted it, when).
CREATE TABLE item_facts (
    item_id    text        NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    fact_id    bigint      NOT NULL REFERENCES checklist_facts(id) ON DELETE CASCADE,
    checked_by text        REFERENCES users(id) ON DELETE SET NULL,
    checked_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (item_id, fact_id)
);

-- A pending transition: the gated status an item is trying to enter but whose
-- checklist isn't yet satisfied. NULL when the item has no pending move. Cleared
-- when the move completes (last fact ticked), is forced, or is cancelled.
ALTER TABLE items ADD COLUMN pending_status_id text REFERENCES statuses(id) ON DELETE SET NULL;
