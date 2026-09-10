-- Personal view collections are initialized once per account/workspace.
CREATE TABLE task_view_collections (
 account_id uuid NOT NULL REFERENCES accounts(id),
 workspace_id uuid NOT NULL REFERENCES workspaces(id),
 PRIMARY KEY(account_id,workspace_id)
);
CREATE TABLE task_views (
 id uuid PRIMARY KEY,
 account_id uuid NOT NULL,
 workspace_id uuid NOT NULL,
 name text NOT NULL CHECK(length(name) BETWEEN 1 AND 60),
 filters jsonb NOT NULL CHECK(jsonb_typeof(filters)='object'),
 position integer NOT NULL,
 version bigint NOT NULL DEFAULT 1,
 FOREIGN KEY(account_id,workspace_id) REFERENCES task_view_collections(account_id,workspace_id) ON DELETE CASCADE,
 UNIQUE(account_id,workspace_id,position)
);
CREATE UNIQUE INDEX task_view_name ON task_views(account_id,workspace_id,lower(name));
