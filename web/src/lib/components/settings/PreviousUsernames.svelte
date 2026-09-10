<script lang="ts">
  let {
    names,
    onChoose,
    busy,
    canChoose = () => true,
  }: {
    names: string[];
    onChoose?: (name: string) => void;
    busy: boolean;
    canChoose?: (name: string) => boolean;
  } = $props();
</script>

{#if names.length > 0}
  <details class="previous-usernames">
    <summary>
      <svg
        viewBox="0 0 16 16"
        width="14"
        height="14"
        fill="none"
        stroke="currentColor"
        stroke-width="1.5"
        aria-hidden="true"><path d="m6 4 4 4-4 4" /></svg
      >
      Previous usernames <span class="count">· {names.length}</span>
    </summary>
    <div class="history-content">
      <p class="hint">These usernames are reserved for this account.</p>
      <ul>
        {#each names as name (name)}
          <li>
            <span class="username">@{name}</span>
            {#if onChoose && canChoose(name)}<button
                type="button"
                disabled={busy}
                aria-label={`Use @${name} again`}
                onclick={() => onChoose?.(name)}>Use again</button
              >{/if}
          </li>
        {/each}
      </ul>
    </div>
  </details>
{/if}

<style>
  .previous-usernames {
    margin-top: 2px;
  }
  summary {
    display: flex;
    align-items: center;
    gap: 6px;
    min-height: 36px;
    width: fit-content;
    max-width: 100%;
    border-radius: 4px;
    cursor: pointer;
    list-style: none;
    font-size: 12px;
    font-weight: 550;
    color: var(--muted);
  }
  summary::-webkit-details-marker {
    display: none;
  }
  summary:hover {
    color: var(--text);
  }
  summary svg {
    flex-shrink: 0;
    transition: transform 150ms ease;
  }
  details[open] summary svg {
    transform: rotate(90deg);
  }
  .count {
    font-weight: 400;
    white-space: nowrap;
  }
  .history-content {
    padding: 2px 0 0 20px;
  }
  ul {
    list-style: none;
    padding: 0;
    margin: 8px 0 0;
  }
  li {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
    min-height: 40px;
  }
  .username {
    min-width: 0;
    overflow-wrap: anywhere;
    color: var(--subtle-text);
    font-size: 13px;
  }
  button {
    flex-shrink: 0;
    border: 0;
    border-radius: 5px;
    padding: 8px;
    margin-right: -8px;
    background: transparent;
    color: var(--accent);
    font-size: 12px;
  }
  button:hover:not(:disabled) {
    background: var(--hover-surface);
  }
</style>
