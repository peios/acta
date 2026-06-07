-- 0029_documents: long-form markdown documents attached to an item — compliance
-- reports, findings, runbooks. Unlike the single description body, an item can
-- carry many named documents; unlike comments, they're titled artifacts edited
-- in place (no per-author lock, no soft-delete). The body is markdown text,
-- rendered through the same pipeline as descriptions and comments. Deleting the
-- item cascades; clearing an author keeps the document (authorship is decorative).
CREATE TABLE documents (
    id         text        PRIMARY KEY DEFAULT gen_id(),
    item_id    text        NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    author_id  text        REFERENCES users(id) ON DELETE SET NULL,
    title      text        NOT NULL,
    body       text        NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX documents_item_idx ON documents (item_id, created_at);
