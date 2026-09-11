<script lang="ts">
  import { onMount } from "svelte";
  import { focusIndicators } from "$lib/focus-indicators";
  onMount(focusIndicators);
  import { mobileViewport } from "$lib/mobile-viewport";
  import DetailViewer from "$lib/components/DetailViewer.svelte";
  import CreateTaskDialog from "$lib/components/tasks/CreateTaskDialog.svelte";
  import ThreadComposerReview from "./ThreadComposerReview.svelte";
  import { provideAccount } from "$lib/account-context";
  import type { Account } from "$lib/api";
  provideAccount({
    account: { id: "mobile-review" } as Account,
    update: () => {},
  });
  let selected = $state("");
  let createdOpened = $state(0);
  let immediateFocus = $state(false);
  let create: CreateTaskDialog;
  const config = {
    prefix: "QA",
    previous_prefixes: [],
    statuses: [{ id: "todo", name: "To do" }],
    creation_status: "todo",
    completed_status: "done",
    version: 1,
    revision: 1,
  };
  function open() {
    history.pushState({}, "", "?task=one");
    selected = "one";
  }
  function close() {
    history.back();
  }
  onMount(() => {
    const changed = () =>
      (selected = new URL(location.href).searchParams.get("task") || "");
    changed();
    window.addEventListener("popstate", changed);
    return () => window.removeEventListener("popstate", changed);
  });
</script>

<div class="shell" use:mobileViewport>
  <header>
    <strong>Mobile review</strong><button
      onclick={() => {
        create.open();
        immediateFocus = !!document.activeElement?.matches("dialog input");
      }}>New task</button
    >
  </header>
  <main>
    <div class="board">
      {#each [1, 2, 3] as column}<section>
          <h2>Column {column}</h2>
          {#each Array(12) as _, index}<button class="task" onclick={open}
              >Task {column}-{index}</button
            >{/each}
        </section>{/each}
    </div>
  </main>
  <footer><ThreadComposerReview /></footer>
</div>
<DetailViewer
  {selected}
  title="QA-1"
  label="task"
  available={390}
  storageKey="mobile-review"
  onclose={close}
>
  <div class="details">
    <h1>Task details</h1>
    {#each Array(15) as _, i}<p>Detail paragraph {i}</p>{/each}<label
      >Comment<textarea aria-label="Comment"></textarea></label
    ><button>Save comment</button>
  </div>
</DetailViewer>
<CreateTaskDialog
  bind:this={create}
  workspace="mobile"
  {config}
  oncreated={() => createdOpened++}
/>
<span data-testid="created-opened" hidden>{createdOpened}</span>
<span data-testid="immediate-focus" hidden>{String(immediateFocus)}</span>

<style>
  .shell {
    position: fixed;
    top: var(--mobile-viewport-top, 0px);
    inset-inline: 0;
    height: var(--mobile-viewport-height, 100dvh);
    display: flex;
    flex-direction: column;
  }
  header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 12px;
  }
  main {
    overflow: auto;
    min-height: 0;
    flex: 1;
  }
  .board {
    display: flex;
    overflow: auto;
    gap: 16px;
    padding: 16px;
  }
  section {
    flex: 0 0 300px;
  }
  .task {
    display: block;
    width: 100%;
    padding: 24px;
    margin: 8px 0;
  }
  footer {
    padding: 12px;
  }
  .details {
    padding: 16px;
  }
  .details p {
    margin-block: 35px;
  }
  textarea {
    display: block;
    width: 100%;
  }
</style>
