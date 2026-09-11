<script lang="ts">
  import ThreadHeader from "$lib/components/threads/ThreadHeader.svelte";
  import { provideNavigation } from "$lib/navigation-context";
  provideNavigation({ open: () => {} });
  let showDebug = $state(false);
  let action = $state("");
  let thread = $state({
    id: "review",
    name: "Pekit tooling",
    provider: "claude",
    cwd: "/home/jack/projects/peios",
    provider_id: "provider",
    run_id: "run",
    state: "running",
    created_at: "2026-09-11T00:00:00Z",
    revision: 1,
    committed: true,
    connection_id: "connection",
  });
  const usage = [
    {
      id: "ctx",
      label: "Ctx",
      title: "Context usage",
      percent: 27,
      details: [],
      reportedAt: null,
    },
    {
      id: "week",
      label: "7d",
      title: "Weekly usage",
      percent: 32,
      details: [],
      reportedAt: null,
    },
    {
      id: "fable",
      label: "Fable",
      title: "Fable usage",
      percent: 32,
      details: [],
      reportedAt: null,
    },
  ];
</script>

<div style="padding:16px">
  <ThreadHeader
    {thread}
    {usage}
    bind:showDebug
    busy={false}
    settingsBusy={false}
    pending={false}
    control={(value) => {
      action = value;
      thread.state = "stopped";
    }}
    onrename={async () => {}}
    ondelete={async () => {}}
  />
  <output hidden>{action}:{String(showDebug)}</output>
</div>
