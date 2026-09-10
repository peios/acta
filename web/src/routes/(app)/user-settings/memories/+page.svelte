<script lang="ts">
  import { onMount } from "svelte";
  import { api, errorMessage } from "$lib/api";
  import MemoryDetail from "$lib/components/memories/MemoryDetail.svelte";
  import type { Memory } from "$lib/memories";
  type Page = { memories: Memory[]; cursor?: string };
  let rows = $state<Memory[]>([]),
    workspaces = $state<{ id: string; name: string }[]>([]),
    agents = $state<{ id: string; username: string }[]>([]);
  let workspace = $state(""),
    agent = $state(""),
    query = $state(""),
    cursor = $state(""),
    error = $state(""),
    busy = $state(false),
    loading = $state(true);
  let detailEditing = $state(false);
  let current = $state<Memory | null>(null),
    editing = $state(false);
  let scope = $state("workspace"),
    key = $state(""),
    summary = $state(""),
    content = $state("");
  let generation = 0,
    selectionGeneration = 0;
  async function load(more = false) {
    const g = ++generation;
    loading = true;
    error = "";
    if (!more) rows = [];
    try {
      const p = new URLSearchParams({ query });
      if (workspace) p.set("workspace", workspace);
      if (agent) p.set("agent_id", agent);
      if (more && cursor) p.set("cursor", cursor);
      const result = await api<Page>("memories?" + p);
      if (g !== generation) return;
      rows = more ? [...rows, ...result.memories] : result.memories;
      cursor = result.cursor || "";
    } catch (e) {
      if (g === generation) error = errorMessage(e);
    } finally {
      if (g === generation) loading = false;
    }
  }
  async function open(row: Memory) {
    if (busy) return;
    const g = ++selectionGeneration;
    error = "";
    try {
      const next = await api<Memory>("memories/" + row.id);
      if (g !== selectionGeneration) return;
      current = next;
      editing = false;
    } catch (e) {
      if (g === selectionGeneration) error = errorMessage(e);
    }
  }
  function create() {
    selectionGeneration++;
    current = null;
    scope = workspace ? "workspace" : "";
    key = "";
    summary = "";
    content = "";
    editing = true;
    error = "";
  }
  async function save() {
    busy = true;
    error = "";
    try {
      const saved = await api<Memory>("memories", {
        id: current?.id,
        scope,
        workspace:
          scope === "workspace" ? current?.scope_id || workspace : null,
        agent_id: scope === "agent" ? current?.scope_id || agent : "",
        key,
        summary,
        content,
        revision: current?.revision || 0,
      });
      current = saved;
      editing = false;
      await load();
    } catch (e) {
      error = errorMessage(e);
    } finally {
      busy = false;
    }
  }
  onMount(() => {
    let alive = true;
    (async () => {
      try {
        const a = await api<{ agents: { id: string; username: string }[] }>(
          "agents",
        );
        if (alive) agents = a.agents;
        let offset = 0;
        for (;;) {
          const w = await api<{
            workspaces: { id: string; name: string }[];
            more: boolean;
          }>("workspaces?offset=" + offset);
          if (!alive) return;
          workspaces = [...workspaces, ...w.workspaces];
          if (!w.more || !w.workspaces.length) break;
          offset += w.workspaces.length;
        }
      } catch (e) {
        if (alive) error = errorMessage(e);
      }
    })();
    load();
    return () => {
      alive = false;
      generation++;
      selectionGeneration++;
    };
  });
</script>

<svelte:head><title>Memories · Acta</title></svelte:head>
<section class="memories">
  <header>
    <p>Durable knowledge, shared in the right place.</p>
    <button
      class="primary"
      onclick={create}
      disabled={busy || editing || detailEditing}>New memory</button
    >
  </header>
  <form
    class="filters"
    onsubmit={(e) => {
      e.preventDefault();
      load();
    }}
  >
    <label
      >Workspace<select
        bind:value={workspace}
        disabled={editing || busy || detailEditing}
        onchange={() => load()}
        ><option value="">Personal & site</option>{#each workspaces as w}<option
            value={w.id}>{w.name}</option
          >{/each}</select
      ></label
    >
    <label
      >Agent<select
        bind:value={agent}
        disabled={editing || busy || detailEditing}
        onchange={() => load()}
        ><option value="">No agent scope</option>{#each agents as a}<option
            value={a.id}>{a.username}</option
          >{/each}</select
      ></label
    >
    <label class="search"
      >Search<input
        bind:value={query}
        placeholder="Find a memory…"
        maxlength="500"
      /></label
    ><button type="submit" disabled={busy}>Search</button>
  </form>
  {#if error}<p class="notice error" role="alert">{error}</p>{/if}
  <div class="memory-layout">
    <div class="memory-list">
      <div class="list-caption">
        Memory index <span>{rows.length}{cursor ? "+" : ""}</span>
      </div>
      {#each rows as row (row.id)}<button
          class="memory-row"
          class:chosen={current?.id === row.id}
          disabled={busy || editing || detailEditing}
          onclick={() => open(row)}
          ><span class="scope">{row.scope}</span><strong>{row.key}</strong><span
            >{row.summary}</span
          ></button
        >{/each}
      {#if loading}<p role="status">
          Loading memories…
        </p>{:else if !rows.length}<p class="empty">
          No memories here yet.
        </p>{/if}
      {#if cursor}<button
          onclick={() => load(true)}
          disabled={loading || busy || editing || detailEditing}
          >Load more</button
        >{/if}
    </div>
    <article>
      {#if editing}
        <form
          onsubmit={(e) => {
            e.preventDefault();
            save();
          }}
          class="editor"
        >
          <div class="editor-heading">
            <h2>{current ? "Edit memory" : "New memory"}</h2>
            <span class="scope"
              >{current
                ? "Revision " + current.revision
                : "Choose its home carefully"}</span
            >
          </div>
          <label
            >Scope<select
              bind:value={scope}
              disabled={!!current || busy}
              required
              ><option value="" disabled>Choose a scope</option><option
                value="workspace"
                disabled={!workspace}>Workspace</option
              ><option value="user">User</option><option
                value="agent"
                disabled={!agent}>Agent</option
              ><option value="site">Site</option></select
            ></label
          >
          <p class="guidance">
            {scope === ""
              ? "Select a workspace above for normal project knowledge, or explicitly choose another scope."
              : scope === "workspace"
                ? "The normal home for project conventions and shared knowledge."
                : scope === "user"
                  ? "Only knowledge truly specific to this person across projects."
                  : scope === "agent"
                    ? "Rare: knowledge specific to this agent’s identity or role. Learning it yourself does not make it agent-specific."
                    : "Rare: knowledge truly global across this entire Acta installation."}
            Task-specific decisions and progress belong on the task.
          </p>
          <label
            >Key<input
              bind:value={key}
              maxlength="100"
              required
              placeholder="build-conventions"
              disabled={busy}
            /></label
          >
          <label
            >Summary<input
              bind:value={summary}
              maxlength="500"
              required
              placeholder="When to use this knowledge"
              disabled={busy}
            /></label
          >
          <label
            >Content <span class="hint">Markdown supported</span><textarea
              bind:value={content}
              rows="14"
              required
              maxlength="64000"
              disabled={busy}></textarea></label
          >
          <div class="actions">
            <button class="primary" disabled={busy}
              >{busy ? "Saving…" : "Save memory"}</button
            ><button
              type="button"
              onclick={() => (editing = false)}
              disabled={busy}>Cancel</button
            >
          </div>
        </form>
      {:else if current}
        {#key current.id}<MemoryDetail
            memory={current}
            onediting={(value) => (detailEditing = value)}
            onchange={(saved) => {
              current = saved;
              void load();
            }}
            ondelete={() => {
              current = null;
              void load();
            }}
          />{/key}
      {:else}<div class="empty-detail">
          <h2>Knowledge worth keeping</h2>
          <p>
            Select a memory to read it, or save something useful for next time.
          </p>
          <p>
            Use workspace memories for shared project knowledge. Keep
            task-specific work on its task.
          </p>
        </div>{/if}
    </article>
  </div>
</section>

<style>
  .memories {
    max-width: 1250px;
    margin: 0 auto;
  }
  header,
  .editor-heading,
  .actions {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
  }
  header {
    margin-bottom: 22px;
  }
  header p,
  .guidance,
  .empty-detail p {
    color: var(--muted);
  }
  .filters {
    display: flex;
    gap: 12px;
    align-items: end;
    flex-wrap: wrap;
    margin-bottom: 24px;
  }
  label {
    display: flex;
    flex-direction: column;
    gap: 7px;
    font-size: 12px;
    color: var(--muted);
  }
  .search {
    flex: 1;
    min-width: 180px;
  }
  .memory-layout {
    display: grid;
    grid-template-columns: minmax(230px, 320px) minmax(0, 1fr);
    gap: 28px;
  }
  .list-caption {
    display: flex;
    justify-content: space-between;
    color: var(--muted);
    font-size: 12px;
    margin-bottom: 12px;
  }
  .memory-row {
    display: flex;
    flex-direction: column;
    text-align: left;
    gap: 7px;
    width: 100%;
    padding: 14px;
    border: 1px solid transparent;
    background: transparent;
    border-radius: 12px;
    margin-bottom: 6px;
  }
  .memory-row:hover,
  .memory-row.chosen {
    background: var(--surface);
    border-color: var(--border);
  }
  .memory-row > span:last-child {
    color: var(--muted);
    font-size: 12px;
    line-height: 1.5;
  }
  .scope {
    font-size: 11px;
    color: var(--muted);
    text-transform: uppercase;
    letter-spacing: 0.06em;
  }
  article {
    min-width: 0;
    border: 1px solid var(--border);
    background: var(--surface);
    padding: 24px;
    border-radius: 16px;
    align-self: start;
  }
  h2 {
    font-size: 19px;
    overflow-wrap: anywhere;
  }
  .editor {
    display: flex;
    flex-direction: column;
    gap: 16px;
  }
  .editor h2 {
    margin: 0;
  }
  .guidance {
    font-size: 12px;
    line-height: 1.6;
    margin: 0;
  }
  .actions {
    justify-content: flex-start;
  }
  .empty-detail {
    padding: 30px 0;
    line-height: 1.6;
  }
  .hint {
    font-size: 11px;
  }
  input,
  select,
  textarea {
    width: 100%;
    box-sizing: border-box;
  }
  textarea {
    resize: vertical;
    font-family: inherit;
  }
  .memory-row strong {
    overflow-wrap: anywhere;
  }
  .empty {
    color: var(--muted);
    font-size: 13px;
  }
  @media (max-width: 750px) {
    .memory-layout {
      grid-template-columns: 1fr;
    }
    .memory-list {
      max-height: 280px;
      overflow: auto;
    }
    article {
      padding: 18px;
    }
    .filters label {
      flex: 1;
      min-width: 140px;
    }
    header {
      align-items: flex-start;
    }
  }
</style>
