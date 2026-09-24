<script lang="ts">
  import { tick } from "svelte";
  import { useCodePreview } from "$lib/code-preview.svelte";
  import { useCodebases } from "$lib/codebases.svelte";
  import AddCodebaseDialog from "./AddCodebaseDialog.svelte";
  import DirectoryNode from "./DirectoryNode.svelte";
  import StartThreadDialog from "./StartThreadDialog.svelte";
  import { useCodeThreads } from "$lib/code-threads.svelte";
  import ProviderIcon from "$lib/components/threads/ProviderIcon.svelte";
  let {
    collapsed = false,
    onNavigate = () => {},
  }: { collapsed?: boolean; onNavigate?: () => void } = $props();
  const code = useCodePreview();
  const codebases = useCodebases();
  const threads = useCodeThreads();
  let startDialog: StartThreadDialog;
  const selectedCodebase = $derived(
    codebases.state.items.find((item) => item.id === codebases.state.selected),
  );
  let addDialog: AddCodebaseDialog;
  const id = $props.id();
  let tabs: HTMLDivElement;
  async function tabKeys(event: KeyboardEvent) {
    if (!["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key)) return;
    event.preventDefault();
    code.sidebarTab =
      event.key === "Home"
        ? "agents"
        : event.key === "End"
          ? "files"
          : code.sidebarTab === "agents"
            ? "files"
            : "agents";
    await tick();
    tabs.querySelector<HTMLButtonElement>('[aria-selected="true"]')?.focus();
  }
  function selectThread(id: string) {
    threads.state.selected = id;
    code.panel = "conversation";
    onNavigate();
  }
</script>

<div class="code-sidebar" class:collapsed>
  <div class="codebase-controls">
    <div class="codebase-choice">
      <svg
        class="codebase-symbol"
        width="16"
        height="16"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        stroke-width="1.6"
        aria-hidden="true"><path d="M3 7V5h6l2 2h10v13H3z" /></svg
      >
      <select
        aria-label="Switch codebase"
        title={selectedCodebase?.name ?? "Select codebase"}
        value={codebases.state.selected}
        disabled={!codebases.ready || !codebases.state.items.length}
        onchange={(event) => {
          codebases.state.selected = event.currentTarget.value;
          codebases.state.selectedFile = "";
        }}
      >
        {#if !codebases.state.items.length}<option value=""
            >{codebases.state.loading ? "Loading…" : "No codebases"}</option
          >{/if}
        {#each codebases.state.items as item (item.id)}<option value={item.id}
            >{item.name}</option
          >{/each}
      </select>
      <span class="codebase-chevron" aria-hidden="true">⌄</span>
      <button
        class="add-codebase"
        aria-label="Add codebase"
        title="Add codebase"
        disabled={!codebases.ready}
        onclick={() => addDialog.open()}>+</button
      >
    </div>
    <div
      class="tabs"
      bind:this={tabs}
      role="tablist"
      tabindex="-1"
      aria-label="Code sidebar"
      onkeydown={tabKeys}
    >
      <button
        id={`${id}-agents`}
        role="tab"
        aria-selected={code.sidebarTab === "agents"}
        aria-controls={`${id}-panel`}
        tabindex={code.sidebarTab === "agents" ? 0 : -1}
        title="Agents"
        onclick={() => (code.sidebarTab = "agents")}
      >
        <svg
          width="16"
          height="16"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="1.6"
          aria-hidden="true"
          ><rect x="4" y="7" width="16" height="13" rx="4" /><path
            d="M12 3v4M8 12v2m8-2v2M9 17h6"
          /></svg
        ><span>Agents</span>
      </button>
      <button
        id={`${id}-files`}
        role="tab"
        aria-selected={code.sidebarTab === "files"}
        aria-controls={`${id}-panel`}
        tabindex={code.sidebarTab === "files" ? 0 : -1}
        title="Files"
        onclick={() => (code.sidebarTab = "files")}
      >
        <svg
          width="16"
          height="16"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="1.6"
          aria-hidden="true"><path d="M3 7V5h6l2 2h10v13H3z" /></svg
        ><span>Files</span>
      </button>
    </div>
  </div>
  {#if codebases.state.error && !collapsed}<p
      class="codebase-error"
      role="status"
    >
      {codebases.state.error}
      <button onclick={() => codebases.refresh()}>Retry</button>
    </p>{/if}
  <div
    id={`${id}-panel`}
    class="tab-panel"
    role="tabpanel"
    aria-labelledby={`${id}-${code.sidebarTab}`}
    tabindex="0"
  >
    {#if code.sidebarTab === "agents"}
      <div class="explorer-heading">
        <span>{collapsed ? "" : "Threads"}</span><button
          aria-label="Start agent thread"
          title="Start agent thread"
          disabled={!codebases.ready || !selectedCodebase}
          onclick={() => startDialog.open()}>+</button
        >
      </div>
      {#if threads.state.error}<p class="codebase-error" role="status">
          {threads.state.error}
        </p>{/if}
      {#each threads.state.items as thread (thread.id)}
        <button
          class="thread"
          class:active={threads.state.selected === thread.id}
          aria-pressed={threads.state.selected === thread.id}
          aria-label={`${thread.name}, ${thread.state}`}
          title={thread.name}
          onclick={() => selectThread(thread.id)}
        >
          <span class="thread-symbol" aria-hidden="true"
            ><ProviderIcon provider={thread.provider} /></span
          >
          <span class="thread-text"
            ><strong>{thread.name}</strong><small
              >{thread.state} · {new Date(
                thread.created_at,
              ).toLocaleTimeString()}</small
            ></span
          >
        </button>
      {:else}<p class="sample-label">
          {threads.state.loading
            ? "Loading threads…"
            : "No threads in this codebase yet."}
        </p>{/each}
    {:else if collapsed}
      <p class="sample-label" title="Expand the sidebar to browse directories">
        ▱
      </p>
    {:else if !codebases.ready}
      <p class="sample-label">Connect your host to browse its directories.</p>
    {:else if !selectedCodebase}
      <p class="sample-label">
        {codebases.state.loading
          ? "Loading codebases…"
          : "Add a codebase to explore its folders."}
      </p>
    {:else}
      <div class="explorer-heading">
        <span>Folders</span><button
          aria-label="Refresh directory explorer"
          title="Refresh directory explorer"
          onclick={() => {
            codebases.state.refreshVersion++;
            void codebases.refresh();
          }}>↻</button
        >
      </div>
      <div class="file-tree" aria-label="Directory explorer">
        {#key `${codebases.state.host}:${selectedCodebase.id}:${codebases.state.refreshVersion}`}
          <ul>
            {#each selectedCodebase.roots as root (root.id)}
              <DirectoryNode
                host={codebases.state.host}
                codebase={selectedCodebase.id}
                root={root.id}
                name={root.path}
              />
            {/each}
          </ul>
        {/key}
      </div>
    {/if}
  </div>
</div>
<StartThreadDialog
  bind:this={startDialog}
  onStarted={() => {
    code.sidebarTab = "agents";
    code.panel = "conversation";
    onNavigate();
  }}
/>
<AddCodebaseDialog
  bind:this={addDialog}
  onSaved={() => {
    code.sidebarTab = "files";
  }}
/>

<style>
  .code-sidebar {
    min-width: 0;
    min-height: 0;
    flex: 1;
    display: flex;
    flex-direction: column;
  }
  .codebase-controls {
    flex-shrink: 0;
    border: 1px solid var(--panel-border);
    border-radius: 8px;
    margin-bottom: 16px;
    background: var(--sidebar-surface);
  }
  .codebase-choice {
    position: relative;
    display: flex;
    align-items: center;
    min-height: 43px;
    border-bottom: 1px solid var(--panel-border);
  }
  .codebase-choice select {
    appearance: none;
    width: calc(100% - 34px);
    min-height: 43px;
    padding: 10px 28px 10px 34px;
    border: 0;
    border-radius: 7px 7px 0 0;
    background: transparent;
    color: var(--text);
    font-size: 12px;
    font-weight: 550;
    cursor: pointer;
  }
  .codebase-choice select:hover {
    background: var(--hover-surface);
  }
  .codebase-choice select:focus-visible {
    outline-offset: -3px;
  }
  .codebase-symbol {
    position: absolute;
    left: 10px;
    color: var(--muted);
    pointer-events: none;
  }
  .codebase-chevron {
    position: absolute;
    right: 43px;
    color: var(--muted);
    pointer-events: none;
    font-size: 12px;
  }
  .add-codebase {
    width: 34px;
    min-height: 40px;
    padding: 0;
    border: 0;
    background: transparent;
    color: var(--text);
    font-size: 20px;
  }
  .add-codebase:hover {
    background: var(--hover-surface);
  }
  .add-codebase:disabled {
    color: var(--muted);
    opacity: 0.5;
  }
  .codebase-error {
    margin: 8px;
    font-size: 11px;
    color: var(--muted);
  }
  .codebase-error button,
  .explorer-heading button {
    border: 0;
    background: transparent;
    color: var(--text);
    padding: 5px;
  }
  .explorer-heading {
    display: flex;
    align-items: center;
    justify-content: space-between;
    color: var(--muted);
    font-size: 11px;
    margin: 0 7px 5px;
  }
  .explorer-heading button {
    font-size: 19px;
  }
  .file-tree > ul {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  .collapsed .codebase-choice {
    flex-direction: column;
  }
  .collapsed .codebase-choice select {
    width: 100%;
  }
  .collapsed .codebase-choice select {
    padding: 8px 2px;
    text-align: center;
    font-size: 10px;
  }
  .collapsed .codebase-symbol,
  .collapsed .codebase-chevron {
    display: none;
  }
  .tabs {
    display: flex;
    gap: 3px;
    padding: 3px;
    border: 0;
    margin: 0;
  }
  .tabs button {
    display: flex;
    flex: 1;
    align-items: center;
    justify-content: center;
    gap: 7px;
    min-width: 0;
    min-height: 34px;
    border: 0;
    border-radius: 5px;
    background: transparent;
    color: var(--muted);
    font-size: 12px;
  }
  .tabs button[aria-selected="true"] {
    background: var(--scope-active);
    color: var(--text);
    font-weight: 550;
  }
  .tabs button:hover {
    background: var(--hover-surface);
  }
  .tab-panel {
    min-width: 0;
    min-height: 0;
    flex: 1;
    overflow-y: auto;
    overscroll-behavior: contain;
  }
  .thread {
    display: flex;
    align-items: flex-start;
    gap: 10px;
    width: 100%;
    padding: 12px 10px;
    margin: 3px 0;
    color: var(--text);
    background: transparent;
    border: 0;
    border-radius: 7px;
    text-align: left;
  }
  .thread.active {
    background: var(--scope-active);
  }
  .thread:hover {
    background: var(--hover-surface);
  }
  .thread-symbol {
    color: var(--accent);
    font-size: 19px;
    width: 20px;
    flex-shrink: 0;
    line-height: 22px;
  }
  .thread-text {
    display: flex;
    flex-direction: column;
    gap: 6px;
    min-width: 0;
  }
  .thread-text strong {
    font-size: 12px;
    font-weight: 500;
    line-height: 1.5;
    overflow-wrap: anywhere;
  }
  .thread-text small {
    color: var(--muted);
    font-size: 10px;
  }
  .file-tree {
    font-size: 12px;
  }
  .sample-label {
    margin: 18px 10px;
    color: var(--muted);
    font-size: 10px;
  }
  .collapsed .tabs {
    flex-direction: column;
    padding: 2px;
  }
  .collapsed .tabs span {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip-path: inset(50%);
  }
  .collapsed .thread {
    justify-content: center;
    padding: 10px 5px;
  }
  .collapsed .thread-text {
    display: none;
  }
  @media (max-width: 720px) {
    .tabs button {
      min-height: 40px;
    }
  }
</style>
