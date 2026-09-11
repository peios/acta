<script lang="ts">
  import { onMount } from "svelte";
  import TaskBoard from "$lib/components/tasks/TaskBoard.svelte";
  import { swipeNavigation } from "$lib/swipe-navigation";
  import { defaultViewDisplay } from "$lib/task-views.js";
  import type { TaskConfig } from "$lib/tasks";
  let drawer = $state<HTMLDialogElement>();
  let opened = $state("");
  let editable = $state(true);
  let group = $state("status");
  let config = $state<TaskConfig>({
    prefix: "QA",
    previous_prefixes: [],
    statuses: [
      { id: "todo", name: "To do" },
      { id: "doing", name: "In progress" },
      { id: "done", name: "Done" },
    ],
    creation_status: "todo",
    completed_status: "done",
    version: 1,
    revision: 1,
  });
  onMount(() => {
    const changed = () => config.revision++;
    window.addEventListener("acta:tasks-changed", changed);
    return () => window.removeEventListener("acta:tasks-changed", changed);
  });
</script>

<div class="shell" use:swipeNavigation={() => drawer}>
  <header>
    <button onclick={() => drawer?.showModal()}>Navigation</button><strong
      >Mobile board</strong
    >
  </header>
  <dialog bind:this={drawer} aria-label="Navigation">
    <button onclick={() => drawer?.close()}>Close navigation</button>
    <p>Workspaces</p>
    <p>My Agents</p>
    <input aria-label="Navigation input" />
  </dialog>
  <main>
    <p class="controls">
      <label
        ><input type="checkbox" bind:checked={editable} />Allow editing</label
      ><label
        >Group <select aria-label="Group" bind:value={group}
          ><option value="status">Status</option><option value="priority"
            >Priority</option
          ><option value="none">None</option></select
        ></label
      >
    </p>
    <TaskBoard
      workspace="mobile"
      {config}
      display={{ ...defaultViewDisplay(), mode: "board", group }}
      statuses={[]}
      assignees={[]}
      unassigned={false}
      query=""
      onopen={(t) => (opened = t.reference)}
      canEdit={editable}
      canCreate={false}
      assignmentGroups={[
        { id: "high", name: "High", assign_id: "", available: true },
        { id: "low", name: "Low", assign_id: "", available: true },
      ]}
      hover={{ id: "" }}
    />
  </main>
  <output aria-label="Opened task">{opened}</output>
</div>

<style>
  :global(body) {
    margin: 0;
  }
  .shell {
    height: 100dvh;
    display: flex;
    flex-direction: column;
  }
  header {
    padding: 16px;
    display: flex;
    gap: 16px;
  }
  main {
    min-height: 0;
    overflow: auto;
    padding: 16px;
  }
  dialog {
    margin: 0;
    height: 100dvh;
    max-height: none;
    width: 280px;
    max-width: 85vw;
    border: 0;
    background: var(--sidebar-surface);
    color: var(--text);
  }
  dialog::backdrop {
    background: #0006;
  }
  .controls {
    display: flex;
    gap: 12px;
    font-size: 12px;
  }
</style>
