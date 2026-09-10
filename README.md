# Acta 2

A deliberate rebuild of Acta: a self-hosted project tool for humans and agents.
Go serves a compiled Svelte 5 frontend, with PostgreSQL persistence.

Accounts include local passwords, passkeys, MFA, session management, invitations,
recovery, direct permissions and additive groups. The standalone Go CLI supports
profiles and browser-approved device login. A hosted MCP endpoint supports
OAuth connections and identity lookup. User-owned agents have bounded permissions
and can be selected during CLI or MCP authorization. Workspaces provide member-only
access, scoped permissions and inherited agent access. Tasks include nested subtasks, multiple assignees, configurable statuses, live
autosave and CLI/MCP access. A local hyperharness lists connected development machines and discovers Codex and Claude Code providers in User Settings.
Projects and provider execution remain subsequent work.

- [Development and running locally](learn/development.md)
- [Production Compose deployment with Caddy](learn/deployment.md)
- [Account behavior and architecture](learn/accounts.md)
- [Application chrome](learn/chrome.md)
- [CLI profiles and authentication](learn/cli.md)
- [Hosted MCP and OAuth](learn/mcp.md)
- [User-owned agents](learn/agents.md)
- [Connected development harnesses](learn/harnesses.md)
- [Provider threads and raw frames](learn/threads.md)
- [Workspaces and scoped access](learn/workspaces.md)
- [Tasks, editing and agent interfaces](learn/tasks.md)
- [Foundation quality review](learn/quality-review-2026-09-07.md)
- Project records: ACT-50 (foundation), ACT-51 (accounts and authentication), ACT-52 (CLI), ACT-53 (MCP), ACT-54 (agent accounts), ACT-55 (workspaces), ACT-56 (tasks), ACT-57 (quality pass).

```sh
make install
make db
make run
```

Open http://localhost:8081 and use the setup code printed in the server terminal.

Installation recovery: [backup service and restore runbook](learn/backups.md).
