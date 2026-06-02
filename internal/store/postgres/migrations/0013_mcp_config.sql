-- 0013_mcp_config: customisation surface for the MCP integration.
--
-- Two instance-global things an operator can tailor without a redeploy:
--
--   app_settings — a tiny key/value bag. Today it holds the MCP "guide" (the
--   conventions document served as the acta://guide resource) under key
--   'mcp.guide', and a one-shot 'mcp.seeded' flag. An empty/absent guide means
--   "serve the built-in default", so the shipped default keeps improving for
--   anyone who never customised it (default-as-fallback, not default-as-copy).
--
--   mcp_prompts — user-defined MCP prompts, surfaced to clients as slash
--   commands (/mcp__acta__<name>). The two starter prompts (standup, triage)
--   are seeded in Go on first run as ordinary, editable rows — not baked into
--   this migration — so there is a single source of truth for their text and
--   deleting one doesn't make it reappear.

CREATE TABLE app_settings (
    key   text PRIMARY KEY,
    value text NOT NULL DEFAULT ''
);

CREATE TABLE mcp_prompts (
    id          text        PRIMARY KEY DEFAULT gen_id(),
    name        text        NOT NULL UNIQUE,
    title       text        NOT NULL DEFAULT '',
    description text        NOT NULL DEFAULT '',
    body        text        NOT NULL DEFAULT '',
    arguments   jsonb       NOT NULL DEFAULT '[]'::jsonb,
    position    integer     NOT NULL DEFAULT 0,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX mcp_prompts_order_idx ON mcp_prompts (position, created_at);
