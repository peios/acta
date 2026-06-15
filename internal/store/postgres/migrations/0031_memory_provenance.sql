-- 0031_memory_provenance: a memory gains a one-line summary and provenance.
--
-- summary is the short hook shown in the recall index, so an agent can scan
-- what's there without fetching every body. created_by / updated_by record the
-- principal (human or agent) that wrote and last touched it — decorative
-- provenance, especially for shared scopes (workspace, project), cleared to null
-- if that user is later removed (like documents.author_id). created_at /
-- updated_at already exist.
ALTER TABLE memories
    ADD COLUMN summary    text NOT NULL DEFAULT '',
    ADD COLUMN created_by text REFERENCES users(id) ON DELETE SET NULL,
    ADD COLUMN updated_by text REFERENCES users(id) ON DELETE SET NULL;
