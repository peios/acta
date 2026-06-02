-- A disabled user keeps all their rows and attribution (created_by, comments,
-- assignments) but can no longer authenticate: login, passkey, and API-token
-- auth all refuse them, and any live session is dropped on its next request.
-- NULL means active. This is the soft-delete for principals.
ALTER TABLE users ADD COLUMN disabled_at timestamptz;
