<script lang="ts">
  import TaskSearch from "$lib/components/tasks/TaskSearch.svelte";
  import CommentThread from "$lib/components/tasks/CommentThread.svelte";
  import type { ActivityEntry } from "$lib/activity";
  import { onMount } from "svelte";
  let search: TaskSearch;
  let selected = $state("");
  let discussion: CommentThread;
  const root: ActivityEntry = {
    id: "root",
    first_event: "1",
    last_event: "1",
    thread_last_event: "3",
    actor: { id: "user", username: "Reviewer", display_name: null },
    kind: "comment",
    before: {},
    after: {},
    count: 1,
    unread: false,
    started_at: "2026-09-01T00:00:00Z",
    updated_at: "2026-09-01T00:00:00Z",
    reply_count: 2,
    comment: {
      body: "Original discussion",
      version: 1,
      deleted: false,
      edited: false,
      can_edit: false,
      can_delete: false,
    },
  };
  onMount(() => {
    (window as any).review = { open: () => search.open() };
  });
</script>

<button onclick={() => search.open()}>Search tasks</button>
<p data-testid="selected">{selected}</p>
<TaskSearch
  bind:this={search}
  onselect={(r) => (selected = r.id + ":" + (r.comment_id || ""))}
/>
<button onclick={() => discussion.jumpTo("old-reply")}
  >Open matching reply</button
>
<CommentThread
  bind:this={discussion}
  task="task"
  entry={root}
  canComment={false}
  highlighted={true}
  refresh={0}
  onchange={() => {}}
  onlayout={() => {}}
/>

<style>
  :global(body) {
    background: #202020;
    color: #eee;
    font-family: system-ui;
    --surface: #262626;
    --muted: #a6a6a6;
    --text: #e8e8e8;
    --panel-border: #383838;
    --accent: #bccadd;
    --hover-surface: #303030;
    --danger: #ffaaaa;
  }
  :global(button) {
    cursor: pointer;
    font: inherit;
  }
</style>
