-- 0018_comment_edit_delete: let authors edit and soft-delete their own comments.
--
-- edited_at (NULL = never edited) stamps the last edit, surfaced as an "(edited)"
-- tag. deleted_at (NULL = live) soft-deletes a comment: the row stays for the
-- append-only audit trail, but the feed renders a tombstone in its place and the
-- default Comments query (MCP, API) drops it entirely.

ALTER TABLE comments
    ADD COLUMN edited_at  timestamptz,
    ADD COLUMN deleted_at timestamptz;
