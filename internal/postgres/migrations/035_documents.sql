CREATE TABLE documents (
 id uuid PRIMARY KEY,
 task_id uuid NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
 revision bigint NOT NULL CHECK(revision>0)
);
CREATE INDEX documents_task ON documents(task_id,id);
CREATE TABLE document_versions (
 file_id uuid PRIMARY KEY,
 document_id uuid NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
 revision bigint NOT NULL CHECK(revision>0),
 title text NOT NULL,
 filename text NOT NULL,
 media_type text NOT NULL,
 size bigint NOT NULL CHECK(size BETWEEN 0 AND 20971520),
 sha256 text NOT NULL,
 created_by uuid NOT NULL REFERENCES accounts(id),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(document_id,revision)
);
-- Bytes are excluded from metadata/history queries and all activity records.
CREATE TABLE document_files (
 file_id uuid PRIMARY KEY REFERENCES document_versions(file_id) ON DELETE CASCADE,
 content bytea NOT NULL CHECK(octet_length(content)<=20971520)
);
