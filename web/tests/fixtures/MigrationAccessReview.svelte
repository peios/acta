<script lang="ts">
  import SessionList from "$lib/components/settings/SessionList.svelte";
  import type { SecurityState } from "$lib/security";
  const params = new URLSearchParams(window.location.search);
  const canMigrate = params.get("superuser") !== "false";
  const sessions: SecurityState["sessions"] = [
    {
      id: "migration-session",
      description: "Migration review client",
      kind: "mcp",
      current: false,
      created_at: "2026-09-10T12:00:00Z",
      last_seen_at: "2026-09-10T12:00:00Z",
      tool_grants: params.has("existing")
        ? ["identity.read", "migration.assistant"]
        : ["identity.read"],
    } as SecurityState["sessions"][number],
  ];
</script>

<main style="max-width:760px;margin:32px auto;padding:0 24px;">
  <h1>Security</h1>
  <SessionList
    {sessions}
    {canMigrate}
    onRevoke={() => {}}
    onAccessChanged={() => {}}
  />
</main>
