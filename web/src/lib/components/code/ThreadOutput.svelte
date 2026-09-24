<script lang="ts">
  import { useCodeThreads, codeProviderName } from "$lib/code-threads.svelte";
  import { useCodebases } from "$lib/codebases.svelte";
  import CodeComposer from "./CodeComposer.svelte";
  import DebugLog from "./DebugLog.svelte";
  import HookFrame from "./HookFrame.svelte";
  const threads = useCodeThreads(),
    codebases = useCodebases();
  const selected = $derived(
    threads.state.items.find((t) => t.id === threads.state.selected),
  );
  let showDebug = $state(true);
  let showSuperseded = $state(false);
  const acknowledged = $derived(
    new Set(
      threads.state.frames
        .filter((f) => f.type === "message.user")
        .map((f) => f.input_id),
    ),
  );
  const visibleFrames = $derived(
    threads.state.frames.filter(
      (f) =>
        (showDebug || !f.type.startsWith("debug.")) &&
        (showSuperseded || !f.superseded_by) &&
        !(f.type === "input.message.user" && acknowledged.has(f.id)),
    ),
  );
  function pretty(raw: string) {
    try {
      return JSON.stringify(JSON.parse(raw), null, 2);
    } catch {
      return raw;
    }
  }
</script>

<section class="thread-output" aria-label="Agent thread output">
  {#if selected}
    <header>
      <div>
        <h2>{selected.name}</h2>
        <p title={selected.directory}>{selected.directory}</p>
      </div>
      <span class="state"
        >{codebases.ready
          ? selected.busy
            ? "running"
            : selected.state
          : "Host disconnected"}</span
      >
    </header>
    <div class="output-toolbar">
      <span>{threads.state.frames.length} frames · r{selected.revision}</span>
      <button
        class="reprocess"
        type="button"
        disabled={!codebases.ready ||
          selected.busy ||
          selected.state === "starting" ||
          !!threads.state.reprocessing}
        onclick={() => void threads.reprocess(selected.id, selected.revision)}
        title="Rebuild this thread's CAT frames from retained output without running the provider"
        >{threads.state.reprocessing === selected.id
          ? "Reprocessing…"
          : "Reprocess"}</button
      >
      <label
        ><input type="checkbox" bind:checked={showDebug} /> Show debug frames</label
      >
      <label
        ><input type="checkbox" bind:checked={showSuperseded} /> Show superseded</label
      >
    </div>
    {#if selected.error}<p class="error" role="alert">{selected.error}</p>{/if}
    {#if threads.state.reprocessError}<p class="error" role="alert">
        {threads.state.reprocessError}
      </p>{/if}
    {#if threads.state.frameError}<p class="error" role="status">
        {threads.state.frameError}
      </p>{/if}
    <div class="frames">
      {#if selected.source_truncated}<p class="note">
          Earlier source records have left the live buffer. Reprocessing covers
          retained history only.
        </p>{/if}
      {#if threads.state.dropped}<p class="note">
          Older frames have left the live buffer. Showing retained output.
        </p>{/if}
      {#each visibleFrames as frame (frame.seq)}
        {#if frame.type === "debug.log"}
          <DebugLog {frame} />
        {:else if frame.hook && frame.type.startsWith("hook.")}
          <HookFrame
            {frame}
            active={codebases.ready &&
              (selected.state === "ready" || selected.state === "starting")}
          />
        {:else}
          <article
            class="frame"
            aria-label={`Frame ${frame.seq}: ${frame.type}`}
          >
            <div class="frame-heading">
              <strong>{frame.type}</strong><span
                >#{frame.seq}{frame.stream ? ` · ${frame.stream}` : ""}</span
              ><span title="Original retained source record"
                >source #{frame.source_seq}</span
              ><time datetime={frame.at}
                >{new Date(frame.at).toLocaleTimeString()}</time
              >
            </div>
            <pre>{frame.raw !== undefined
                ? pretty(frame.raw)
                : JSON.stringify(
                    {
                      id: frame.id,
                      input_id: frame.input_id,
                      text: frame.text,
                      delivery: frame.delivery,
                      turn_id: frame.turn_id,
                      error: frame.error,
                    },
                    null,
                    2,
                  )}</pre>
          </article>
        {/if}
      {:else}<p class="empty">
          {selected.state === "starting"
            ? `Initializing ${codeProviderName(selected.provider)}…`
            : "No output yet. The provider can remain quiet while waiting for input."}
        </p>{/each}
    </div>
    {#key selected.id}<CodeComposer
        provider={selected.provider}
        disabled={!codebases.ready ||
          selected.state !== "ready" ||
          threads.state.reprocessing === selected.id}
        busy={selected.busy}
        onSend={(id, text) => threads.send(selected.id, id, text)}
      />{/key}
  {:else}<div class="empty">
      <h2>
        {threads.state.loading ? "Loading threads…" : "Start an agent thread"}
      </h2>
      <p>Use + in Agents to start a thread in this codebase.</p>
    </div>{/if}
</section>

<style>
  .thread-output {
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
    height: 100%;
  }
  header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
    padding: 20px 24px;
    border-bottom: 1px solid var(--panel-border);
  }
  header > div {
    min-width: 0;
  }
  h2 {
    margin: 0;
    font-size: 14px;
    font-weight: 550;
  }
  header p {
    color: var(--muted);
    font:
      11px ui-monospace,
      monospace;
    margin: 8px 0 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .state {
    font-size: 11px;
    color: var(--muted);
    white-space: nowrap;
  }
  .output-toolbar {
    padding: 12px 24px;
    display: flex;
    gap: 16px;
    justify-content: space-between;
    color: var(--muted);
    font-size: 11px;
    flex-wrap: wrap;
    align-items: center;
  }
  .reprocess {
    padding: 4px 8px;
    min-height: 0;
    font-size: 11px;
    color: var(--subtle-text);
    background: var(--hover-surface);
    border: 1px solid var(--panel-border);
    border-radius: 5px;
  }
  .reprocess:disabled {
    opacity: 0.45;
    cursor: default;
  }
  label {
    display: flex;
    gap: 6px;
    align-items: center;
    margin: 0;
    font-size: 11px;
    font-weight: 400;
    white-space: nowrap;
  }
  input {
    margin: 0;
    padding: 0;
    width: 14px;
    height: 14px;
    min-height: 0;
    flex: 0 0 14px;
    accent-color: var(--accent);
  }
  .frames {
    flex: 1;
    min-height: 0;
    overflow: auto;
    padding: 4px 24px 24px;
  }
  .frame {
    border: 1px solid var(--panel-border);
    border-radius: 8px;
    margin-bottom: 12px;
  }
  .frame-heading {
    display: flex;
    flex-wrap: wrap;
    gap: 12px;
    padding: 10px 12px;
    border-bottom: 1px solid var(--panel-border);
    color: var(--muted);
    font:
      10px ui-monospace,
      monospace;
  }
  .frame-heading strong {
    color: var(--subtle-text);
    font-weight: 500;
  }
  time {
    margin-left: auto;
  }
  pre {
    margin: 0;
    padding: 12px;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    color: var(--subtle-text);
    font:
      11px/1.6 ui-monospace,
      monospace;
  }
  .empty {
    padding: 32px 24px;
    color: var(--muted);
    font-size: 13px;
    line-height: 1.7;
  }
  .error {
    color: var(--danger);
    margin: 12px 24px;
    font-size: 12px;
  }
  .note {
    color: var(--muted);
    font-size: 11px;
  }
</style>
