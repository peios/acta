-- 0010_agents: agent principals and item authorship.
--
-- An agent is just another user whose sole credential is a personal access
-- token, acting on behalf of a human. agent_of_id points at that human; a row
-- is an agent exactly when agent_of_id IS NOT NULL. Humans have it NULL. The
-- username carries the rendered "owner/agentname" handle, but ownership lives
-- in this FK so it survives and stays queryable. Deleting a human removes its
-- agents (CASCADE).
--
-- created_by records which principal (human or agent) created an item, so work
-- can be attributed. It's nullable: items predating this column, and items
-- whose creator was later deleted, simply have no recorded author (SET NULL).

ALTER TABLE users
    ADD COLUMN agent_of_id text NULL REFERENCES users(id) ON DELETE CASCADE;

CREATE INDEX users_agent_of_id_idx ON users (agent_of_id);

ALTER TABLE items
    ADD COLUMN created_by text NULL REFERENCES users(id) ON DELETE SET NULL;
