<script lang="ts">
  import { onDestroy, tick } from "svelte";
  import { dismissDialogBackdrop } from "$lib/dialog-backdrop";
  import { api, errorMessage } from "$lib/api";
  import { LatestRequest } from "$lib/requests.js";
  import type { TaskSearchPage, TaskSearchResult } from "$lib/task-search";
  import OptionPicker from "../OptionPicker.svelte";
  let { onselect }: { onselect: (result: TaskSearchResult) => void } = $props();
  let dialog: HTMLDialogElement, input: HTMLInputElement;
  let opened = $state(false),
    query = $state(""),
    workspace = $state(""),
    results = $state<TaskSearchResult[]>([]);
  let includeArchived = $state(false);
  let loading = $state(false),
    more = $state(false),
    cursor = $state(""),
    error = $state(""),
    scopeError = $state("");
  let choices = $state([{ value: "", label: "All workspaces" }]),
    active = $state(0);
  let opener: HTMLElement | null = null;
  const id = $props.id(),
    requests = new LatestRequest(),
    scopes = new LatestRequest();
  export async function open() {
    if (opened) {
      input.focus();
      return;
    }
    opener = document.activeElement as HTMLElement;
    opened = true;
    dialog.showModal();
    await tick();
    input.focus();
    input.select();
    void loadScopes();
  }
  function close() {
    opened = false;
    requests.begin();
    scopes.begin();
    dialog.close();
    opener?.focus({ preventScroll: true });
  }
  async function loadScopes() {
    const read = scopes.begin();
    scopeError = "";
    try {
      const next = [{ value: "", label: "All workspaces" }];
      let offset = 0;
      for (;;) {
        const p = await api<{
          workspaces: { id: string; name: string }[];
          more: boolean;
        }>(`workspaces?offset=${offset}`, undefined, { signal: read.signal });
        if (!read.current()) return;
        next.push(...p.workspaces.map((w) => ({ value: w.id, label: w.name })));
        if (!p.more || !p.workspaces.length) break;
        offset += p.workspaces.length;
      }
      choices = next;
    } catch (e) {
      if (read.current()) scopeError = errorMessage(e);
    }
  }
  async function search(older = false) {
    const read = requests.begin();
    loading = true;
    error = "";
    const q = new URLSearchParams({
      include_archived: String(includeArchived),
      q: query.trim(),
      workspace,
      cursor: older ? cursor : "",
    });
    try {
      const p = await api<TaskSearchPage>(`tasks/search?${q}`, undefined, {
        signal: read.signal,
      });
      if (!read.current()) return;
      results = older
        ? [
            ...results,
            ...p.tasks.filter((t) => !results.some((r) => r.id === t.id)),
          ]
        : p.tasks;
      more = p.more;
      cursor = p.cursor;
    } catch (e) {
      if (read.current()) error = errorMessage(e);
    } finally {
      if (read.current()) loading = false;
    }
  }
  $effect(() => {
    const q = query.trim(),
      w = workspace,
      isOpen = opened;
    void w;
    void includeArchived;
    requests.begin();
    results = [];
    more = false;
    cursor = "";
    active = 0;
    error = "";
    loading = false;
    if (!isOpen || q.length < 2) return;
    loading = true;
    const timer = setTimeout(() => void search(), 200);
    return () => clearTimeout(timer);
  });
  function choose(r: TaskSearchResult) {
    close();
    onselect(r);
  }
  async function keys(e: KeyboardEvent) {
    if (e.isComposing) return;
    const controlKey = e.ctrlKey && !e.altKey && !e.metaKey;
    const next =
      e.key === "ArrowDown" || (controlKey && e.key.toLowerCase() === "n");
    const previous =
      e.key === "ArrowUp" || (controlKey && e.key.toLowerCase() === "p");
    if (next || previous) {
      e.preventDefault();
      if (!results.length) return;
      active = (active + (next ? 1 : -1) + results.length) % results.length;
      await tick();
      dialog
        .querySelector(`#${id}-result-${active}`)
        ?.scrollIntoView({ block: "nearest" });
    } else if (e.key === "Enter") {
      e.preventDefault();
      if (results[active] && !loading) choose(results[active]);
    }
  }
  onDestroy(() => {
    requests.dispose();
    scopes.dispose();
  });
</script>

<dialog
  bind:this={dialog}
  class="task-search"
  aria-label="Search tasks"
  oncancel={(e) => {
    e.preventDefault();
    close();
  }}
  onclick={(e) => {
    if (!window.matchMedia("(max-width: 759px)").matches)
      dismissDialogBackdrop(e, dialog, close);
  }}
>
  <div class="search-input">
    <svg
      width="21"
      height="21"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      stroke-width="1.6"
      aria-hidden="true"
      ><circle cx="10.5" cy="10.5" r="6.5" /><path d="m16 16 5 5" /></svg
    >
    <input
      bind:this={input}
      bind:value={query}
      maxlength="200"
      placeholder="Find a task…"
      aria-label="Search tasks"
      role="combobox"
      aria-autocomplete="list"
      aria-expanded={opened}
      aria-controls={`${id}-results`}
      aria-activedescendant={results[active]
        ? `${id}-result-${active}`
        : undefined}
      onkeydown={keys}
    />
    <button class="close" aria-label="Close search" onclick={close}>
      <span class="close-label">Esc</span>
      <svg
        class="close-icon"
        width="20"
        height="20"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        stroke-width="1.8"
        stroke-linecap="round"
        aria-hidden="true"><path d="m6 6 12 12M18 6 6 18" /></svg
      >
    </button>
  </div>
  <div class="workspace-pills" role="group" aria-label="Search workspace">
    {#each choices as choice (choice.value)}
      <button
        class:active={workspace === choice.value}
        aria-pressed={workspace === choice.value}
        onclick={() => {
          workspace = choice.value;
          input.focus({ preventScroll: true });
        }}>{choice.label}</button
      >
    {/each}
  </div>
  <div class="scope">
    <OptionPicker
      label="Search workspace"
      value={workspace}
      options={choices}
      onchange={(v) => (workspace = v)}
    /><label class="archive-option"
      ><input type="checkbox" bind:checked={includeArchived} /> Include archived</label
    >
  </div>
  <div class="search-body">
    {#if scopeError}<p class="error" role="alert">
        {scopeError}<button onclick={() => void loadScopes()}
          >Retry workspaces</button
        >
      </p>{/if}
    <div
      class="results"
      id={`${id}-results`}
      role="listbox"
      aria-label="Matching tasks"
      aria-busy={loading}
    >
      {#each results as result, i (result.id)}
        <button
          id={`${id}-result-${i}`}
          role="option"
          aria-selected={active === i}
          tabindex="-1"
          class:active={active === i}
          onclick={() => choose(result)}
          onpointermove={() => (active = i)}
        >
          <div class="result-heading">
            <span class="reference">{result.reference}</span><strong
              >{result.title}</strong
            ><span class="status"
              >{result.archived ? "Archived · " : ""}{result.status.name}</span
            >
          </div>
          <div class="breadcrumb">
            {result.workspace_name}{#each result.ancestors as parent}<span
                aria-hidden="true"
              >
                ›
              </span>{parent.title}{/each}
          </div>
          {#if result.source === "comment" || result.source === "description"}<p
              class="excerpt"
            >
              <span class="source"
                >{result.source === "comment" ? "Comment" : "Description"} ·
              </span>{#each result.excerpt as part}{#if part.match}<mark
                    >{part.text}</mark
                  >{:else}{part.text}{/if}{/each}
            </p>{/if}
        </button>
      {/each}
    </div>
    {#if error}<p class="error" role="alert">
        {error}<button onclick={() => void search(results.length > 0)}
          >Try again</button
        >
      </p>
    {:else if loading}<p class="empty" role="status">Searching…</p>
    {:else if !results.length}<div class="empty" role="status">
        {query.trim().length < 2
          ? "Search across every workspace and subtask."
          : "No matching tasks. Try different words or another workspace."}
      </div>{/if}
    {#if more}<button
        class="more"
        disabled={loading}
        onclick={() => void search(true)}>Load more results</button
      >{/if}
  </div>
  <footer>
    <span>↑ ↓ Navigate</span><span>↵ Open task</span><span>Esc Close</span>
  </footer>
</dialog>

<style>
  .workspace-pills {
    display: none;
  }
  .archive-option {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 12px;
    color: var(--muted);
    white-space: nowrap;
  }
  .archive-option input {
    width: 14px;
    height: 14px;
    accent-color: var(--accent);
  }
  .breadcrumb > span {
    margin: 0 5px;
  }
  .task-search {
    padding: 0;
    width: min(740px, calc(100vw - 32px));
    max-width: none;
    margin: clamp(20px, 14vh, 140px) auto 24px;
    max-height: 75dvh;
    border: 1px solid var(--panel-border);
    border-radius: 16px;
    background: var(--surface);
    color: var(--text);
    box-shadow: 0 24px 90px #0006;
    overflow: auto;
  }
  .task-search::backdrop {
    background: #0007;
    backdrop-filter: blur(3px);
  }
  .search-input {
    display: flex;
    align-items: center;
    gap: 14px;
    padding: 20px 22px;
    border-bottom: 1px solid var(--panel-border);
    color: var(--muted);
  }
  input {
    min-width: 0;
    flex: 1;
    border: 0 !important;
    background: transparent !important;
    box-shadow: none !important;
    outline: 0;
    font: inherit;
    font-size: 18px;
    color: var(--text);
    padding: 4px 0;
  }
  .close {
    padding: 4px 7px;
    border: 1px solid var(--panel-border);
    border-radius: 5px;
    background: transparent;
    color: var(--muted);
    font-size: 11px;
    min-height: 0;
  }
  .close-icon {
    display: none;
  }
  .scope {
    padding: 12px 18px;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    font-size: 11px;
    color: var(--muted);
  }
  .scope :global(.picker) {
    max-width: 260px;
  }
  .results {
    padding: 0 8px;
  }
  .results > button {
    display: block;
    width: 100%;
    padding: 14px;
    text-align: left;
    border: 0;
    border-radius: 9px;
    color: var(--text);
    background: transparent;
  }
  .results > button.active,
  .results > button:hover {
    background: var(--hover-surface);
  }
  .result-heading {
    display: flex;
    gap: 10px;
    align-items: baseline;
    font-size: 14px;
  }
  .reference {
    font-size: 11px;
    color: var(--muted);
    white-space: nowrap;
  }
  strong {
    font-weight: 550;
    flex: 1;
    overflow-wrap: anywhere;
  }
  .status {
    font-size: 11px;
    white-space: nowrap;
    color: var(--muted);
  }
  .breadcrumb {
    margin-top: 5px;
    font-size: 11px;
    color: var(--muted);
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
  }
  .excerpt {
    font-size: 12px;
    line-height: 1.6;
    margin: 8px 0 0;
    color: var(--muted);
    display: -webkit-box;
    -webkit-line-clamp: 2;
    line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
    overflow-wrap: anywhere;
  }
  .source {
    font-weight: 550;
  }
  mark {
    color: var(--text);
    background: color-mix(in srgb, var(--accent) 18%, transparent);
    border-radius: 2px;
  }
  .empty {
    padding: 36px 24px;
    text-align: center;
    font-size: 13px;
    color: var(--muted);
  }
  .error {
    padding: 14px 22px;
    color: var(--danger);
    font-size: 13px;
  }
  .error button {
    margin-left: 10px;
  }
  .more {
    margin: 8px 22px 16px;
    background: transparent;
    color: var(--accent);
    border: 0;
    font-size: 12px;
    padding: 8px;
  }
  footer {
    display: flex;
    gap: 20px;
    padding: 12px 22px;
    border-top: 1px solid var(--panel-border);
    font-size: 11px;
    color: var(--muted);
  }
  @media (max-width: 759px) {
    .task-search {
      position: fixed;
      top: var(--mobile-viewport-top, 0px);
      left: 0;
      right: 0;
      bottom: auto;
      margin: 0;
      width: 100%;
      height: var(--mobile-viewport-height, 100dvh);
      max-height: none;
      box-sizing: border-box;
      border: 0;
      border-radius: 0;
      overflow: hidden;
    }
    .task-search[open] {
      display: flex;
      flex-direction: column;
    }
    .search-body {
      order: 1;
      flex: 1;
      min-height: 0;
      overflow-y: auto;
      overscroll-behavior: contain;
      padding: 12px env(safe-area-inset-right) 12px env(safe-area-inset-left);
    }
    .scope {
      display: none;
    }
    .workspace-pills {
      display: flex;
      order: 0;
      flex: 0 0 auto;
      gap: 8px;
      overflow-x: auto;
      overscroll-behavior-x: contain;
      padding: max(12px, env(safe-area-inset-top))
        max(12px, env(safe-area-inset-right)) 12px
        max(12px, env(safe-area-inset-left));
      border-bottom: 1px solid var(--panel-border);
      scrollbar-width: none;
    }
    .workspace-pills button {
      flex: 0 0 auto;
      min-height: 44px;
      padding: 8px 16px;
      border: 1px solid var(--panel-border);
      border-radius: 999px;
      background: transparent;
      color: var(--muted);
      font: inherit;
      font-size: 13px;
      white-space: nowrap;
    }
    .workspace-pills button.active {
      background: var(--hover-surface);
      border-color: var(--accent);
      color: var(--text);
    }
    .search-input {
      order: 3;
      flex-shrink: 0;
      gap: 12px;
      padding: 8px max(12px, env(safe-area-inset-right))
        max(12px, env(safe-area-inset-bottom))
        max(16px, env(safe-area-inset-left));
      border-bottom: 0;
      border-top: 1px solid var(--panel-border);
    }
    .search-input > input {
      min-height: 44px;
      font-size: 16px;
    }
    .close {
      display: grid;
      place-items: center;
      flex: 0 0 44px;
      height: 44px;
      padding: 0;
      border-radius: 10px;
      background: var(--hover-surface);
    }
    .close-label,
    footer {
      display: none;
    }
    .close-icon {
      display: block;
    }
  }
  @media (max-width: 600px) {
    .scope {
      gap: 8px;
      flex-wrap: wrap;
    }
    .status {
      max-width: 85px;
      overflow: hidden;
      text-overflow: ellipsis;
    }
    .result-heading {
      flex-wrap: wrap;
    }
    .reference {
      width: auto;
    }
    footer {
      gap: 14px;
    }
  }
</style>
