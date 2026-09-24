<script lang="ts">
  import { onMount, onDestroy } from "svelte";
  import { errorMessage } from "$lib/api";
  import { useCodebases, type Directory } from "$lib/codebases.svelte";
  import DirectoryNode from "./DirectoryNode.svelte";
  let {
    host,
    codebase,
    root,
    path = ".",
    name,
    depth = 0,
  }: {
    host: string;
    codebase: string;
    root: string;
    path?: string;
    name: string;
    depth?: number;
  } = $props();
  const codebases = useCodebases();
  let expanded = $state(false),
    busy = $state(false),
    error = $state("");
  let listing = $state<Directory | null>(null);
  const abort = new AbortController();
  onDestroy(() => abort.abort());
  onMount(() => {
    if (depth === 0) {
      expanded = true;
      void load();
    }
  });
  async function load() {
    if (busy) return;
    busy = true;
    error = "";
    try {
      const result = await codebases.request<Directory>(
        host,
        "directories.list",
        { codebase_id: codebase, root_id: root, path },
        abort.signal,
      );
      if (!abort.signal.aborted) listing = result;
    } catch (e) {
      if (!abort.signal.aborted) error = errorMessage(e);
    } finally {
      if (!abort.signal.aborted) busy = false;
    }
  }
  function toggle() {
    expanded = !expanded;
    if (expanded && !listing) void load();
  }
</script>

<li>
  <button
    class="directory"
    aria-expanded={expanded}
    aria-label={`${expanded ? "Collapse" : "Expand"} ${name}`}
    title={path === "." ? name : path}
    onclick={toggle}
  >
    <span class="chevron" aria-hidden="true">{expanded ? "⌄" : "›"}</span>
    <svg
      width="15"
      height="15"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      stroke-width="1.5"
      aria-hidden="true"><path d="M3 7V5h6l2 2h10v13H3z" /></svg
    >
    <span class="name">{name}</span>
  </button>
  {#if expanded}
    <div class="contents">
      {#if busy}<p role="status">Loading…</p>{/if}
      {#if error}<p role="status">
          {error} <button class="retry" onclick={load}>Retry</button>
        </p>{/if}
      {#if listing}
        <ul>
          {#each listing.entries as entry (entry.name)}
            {@const entryPath =
              path === "." ? entry.name : `${path}/${entry.name}`}
            {#if entry.kind === "directory"}
              <DirectoryNode
                {host}
                {codebase}
                {root}
                path={entryPath}
                name={entry.name}
                depth={depth + 1}
              />
            {:else}
              <li>
                <button
                  class="file"
                  class:chosen={codebases.state.selectedFile ===
                    `${root}:${entryPath}`}
                  aria-pressed={codebases.state.selectedFile ===
                    `${root}:${entryPath}`}
                  title={`${entryPath}${entry.kind === "symlink" ? " · Symbolic link" : ""}`}
                  onclick={() =>
                    (codebases.state.selectedFile = `${root}:${entryPath}`)}
                >
                  <span class="file-icon" aria-hidden="true"
                    >{entry.kind === "symlink" ? "↗" : "·"}</span
                  ><span class="name">{entry.name}</span>
                </button>
              </li>
            {/if}
          {/each}
        </ul>
        {#if !listing.entries.length && !busy}<p>Empty folder</p>{/if}
        {#if listing.truncated}<p>
            Showing the first 2,000 entries in this folder.
          </p>{/if}
      {/if}
    </div>
  {/if}
</li>

<style>
  ul {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  li {
    min-width: 0;
  }
  .directory,
  .file {
    display: flex;
    align-items: center;
    gap: 6px;
    min-height: 32px;
    width: 100%;
    text-align: left;
    padding: 6px 5px;
    border: 0;
    border-radius: 5px;
    color: var(--subtle-text);
    background: transparent;
    font-size: 12px;
  }
  .directory:hover,
  .file:hover {
    background: var(--hover-surface);
  }
  .file.chosen {
    background: var(--scope-active);
    color: var(--text);
  }
  .name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  svg,
  .chevron,
  .file-icon {
    flex-shrink: 0;
  }
  .chevron {
    width: 10px;
  }
  .file-icon {
    width: 23px;
    text-align: center;
    color: var(--muted);
  }
  .contents {
    margin-left: 12px;
    border-left: 1px solid var(--panel-border);
    padding-left: 4px;
  }
  p {
    color: var(--muted);
    font-size: 11px;
    margin: 8px;
    overflow-wrap: anywhere;
  }
  .retry {
    color: var(--text);
    border: 0;
    background: transparent;
    text-decoration: underline;
    padding: 5px;
  }
  @media (max-width: 720px) {
    .directory,
    .file {
      min-height: 40px;
    }
  }
</style>
