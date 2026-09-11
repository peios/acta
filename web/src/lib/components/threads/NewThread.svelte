<script lang="ts">
  import { onMount } from "svelte";
  import { goto } from "$app/navigation";
  import { api, errorMessage } from "$lib/api";
  import { watchHarnesses, type HarnessView } from "$lib/harnesses";
  import { useThreads } from "$lib/threads.svelte";
  const threads = useThreads();
  const titleId = $props.id();
  let harnesses = $state<HarnessView>({
    state: "connecting",
    connections: [],
    message: "",
  });
  let dialog = $state<HTMLDialogElement>();
  let connection = $state("");
  let cwd = $state("");
  let provider = $state("codex");
  let busy = $state(false);
  let error = $state("");
  onMount(() => {
    try {
      cwd = localStorage.getItem("acta.thread.cwd") || "";
    } catch {}
    return watchHarnesses((next) => {
      harnesses = next;
    });
  });
  $effect(() => {
    if (threads.createOpen) dialog?.showModal();
    else dialog?.close();
  });
  $effect(() => {
    if (
      !harnesses.connections.some((c) => c.id === connection) &&
      harnesses.connections.length
    )
      connection = harnesses.connections[0].id;
  });
  $effect(() => {
    const pending = threads.pending;
    if (!pending) return;
    const found = threads.items.find((t) => t.id === pending.id);
    if (found) {
      threads.pending = null;
      threads.notice = "";
      void goto(`/my-agents/${found.id}`);
    }
  });
  onMount(() => {
    const timer = setInterval(() => {
      const p = threads.pending;
      if (!p) return;
      const machine = harnesses.connections.find((c) => c.id === p.connection);
      const failure = machine?.results?.find(
        (r) => r.thread_id === p.id && r.error,
      );
      if (failure) {
        threads.notice = failure.error || "Thread creation failed.";
        threads.pending = null;
      } else if (harnesses.state === "live" && !machine) {
        threads.notice =
          "The harness disconnected before thread creation was confirmed. If the thread becomes available, it will appear in My Agents.";
        threads.pending = null;
      } else if (Date.now() - p.started >= 30000) {
        threads.notice =
          "Thread creation wasn’t confirmed. The harness may still be starting it. If it becomes available, it will appear in My Agents.";
        threads.pending = null;
      }
    }, 250);
    return () => clearInterval(timer);
  });
  async function create(event: SubmitEvent) {
    event.preventDefault();
    busy = true;
    error = "";
    const id = crypto.randomUUID();
    try {
      await api("threads/create", {
        id,
        connection_id: connection,
        provider,
        cwd: cwd.trim(),
      });
      try {
        localStorage.setItem("acta.thread.cwd", cwd.trim());
      } catch {}
      threads.pending = { id, connection, started: Date.now() };
      threads.notice = "";
      threads.createOpen = false;
    } catch (e) {
      error = errorMessage(e);
    } finally {
      busy = false;
    }
  }
</script>

<dialog
  bind:this={dialog}
  class="thread-dialog"
  aria-labelledby={titleId}
  onclose={() => (threads.createOpen = false)}
  oncancel={() => (threads.createOpen = false)}
>
  <form onsubmit={create}>
    <header>
      <h2 id={titleId}>New thread</h2>
      <button
        type="button"
        class="quiet"
        aria-label="Close"
        onclick={() => (threads.createOpen = false)}>×</button
      >
    </header>
    <p>
      Choose where your provider will run. New threads send a one-time “Test
      Message” to start the conversation.
    </p>
    {#if error}<p class="notice error" role="alert">{error}</p>{/if}
    <label
      >Harness<select bind:value={connection} required
        ><option value="" disabled>Select a connected harness</option
        >{#each harnesses.connections as c}<option value={c.id}
            >{c.hostname}</option
          >{/each}</select
      ></label
    >
    <label
      >Provider<select aria-label="Provider" bind:value={provider}
        ><option value="codex">Codex</option><option value="claude"
          >Claude Code</option
        ></select
      ></label
    >
    <label
      >Working directory<input
        bind:value={cwd}
        placeholder="/home/you/project"
        required
        autocomplete="off"
        spellcheck="false"
      /></label
    >
    <footer>
      <button
        type="button"
        class="secondary"
        onclick={() => (threads.createOpen = false)}>Cancel</button
      ><button
        class="primary"
        type="submit"
        disabled={busy || !connection || !cwd.trim()}
        >{busy ? "Requesting…" : "Create thread"}</button
      >
    </footer>
  </form>
</dialog>

<style>
  .thread-dialog {
    width: min(460px, calc(100vw - 32px));
    padding: 28px;
    background: var(--surface);
    color: var(--text);
    border: 1px solid var(--panel-border);
    border-radius: 16px;
    box-shadow: 0 24px 80px #0005;
  }
  .thread-dialog::backdrop {
    background: #0007;
    backdrop-filter: blur(3px);
  }
  header,
  footer {
    display: flex;
    align-items: center;
    gap: 12px;
    justify-content: space-between;
  }
  h2 {
    font-size: 20px;
    margin: 0;
  }
  p {
    color: var(--muted);
    font-size: 13px;
    margin: 12px 0 24px;
  }
  label {
    display: grid;
    gap: 9px;
    font-size: 13px;
    margin-top: 20px;
  }
  input,
  select {
    width: 100%;
    box-sizing: border-box;
  }
  footer {
    justify-content: flex-end;
    margin-top: 28px;
  }
  footer button {
    width: auto;
  }
  header button {
    font-size: 24px;
    padding: 0 8px;
    border: 0;
    border-radius: 6px;
    background: transparent;
    color: var(--muted);
  }
  header button:hover {
    background: var(--hover-surface);
    color: var(--text);
  }
</style>
