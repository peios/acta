-- Stored vectors are updated in the same transaction as the source row.
ALTER TABLE tasks ADD COLUMN search_title tsvector GENERATED ALWAYS AS (to_tsvector('simple',title)) STORED;
ALTER TABLE tasks ADD COLUMN search_document tsvector GENERATED ALWAYS AS (setweight(to_tsvector('english',title),'A') || setweight(to_tsvector('english',description),'B')) STORED;
CREATE INDEX tasks_search_title ON tasks USING gin(search_title);
CREATE INDEX tasks_search_document ON tasks USING gin(search_document);
ALTER TABLE task_comments ADD COLUMN search_document tsvector GENERATED ALWAYS AS (to_tsvector('english',body)) STORED;
CREATE INDEX comments_search_document ON task_comments USING gin(search_document) WHERE NOT deleted;
