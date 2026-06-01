-- 0008_session_user_agent: give each session a non-secret public id and record
-- the user-agent it was established with.
--
-- public_id exists so the account UI can list and revoke sessions by a handle
-- that is safe to put in page HTML — the row's `id` is the bearer token itself
-- and must never leave the server. user_agent makes the session list
-- recognisable ("Firefox on Linux") instead of three identical rows.

ALTER TABLE sessions
    ADD COLUMN public_id  text NOT NULL DEFAULT gen_id(),
    ADD COLUMN user_agent text NOT NULL DEFAULT '';

CREATE UNIQUE INDEX sessions_public_id_idx ON sessions (public_id);
