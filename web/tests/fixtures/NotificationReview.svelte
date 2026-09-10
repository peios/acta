<script lang="ts">
  import TaskFollow from "$lib/components/tasks/TaskFollow.svelte";
  import NotificationBell from "$lib/components/chrome/NotificationBell.svelte";
  import { provideNotifications } from "$lib/notifications.svelte";
  import type { Account } from "$lib/api";
  const view = provideNotifications(() => ({ id: "review" }) as Account);
  import { onMount } from "svelte";
  onMount(() => {
    (window as any).review = view;
  });
</script>

<div
  class="task-actions"
  style="position:fixed;right:32px;top:24px;display:flex;align-items:center;gap:8px"
>
  <span>ACT-97</span><TaskFollow task="review-task" /><span
    aria-label="Task actions">⋮</span
  >
</div>
<div class="sidebar"><NotificationBell /></div>

<style>
  :global(body) {
    background: #202020;
    color: #ddd;
    font-family: system-ui;
    --surface: #262626;
    --muted: #a6a6a6;
    --text: #e8e8e8;
    --panel-border: #383838;
    --accent: #bccadd;
    --hover-surface: #303030;
  }
  .sidebar {
    position: fixed;
    bottom: 24px;
    left: 16px;
    width: 210px;
  }
</style>
