<script lang="ts">
  import { anchoredPopover } from "$lib/anchored-popover";
  import { tick } from "svelte";
  import { completedStatus, entryStatus, type TaskConfig } from "$lib/tasks";

  let {
    value,
    config,
    disabled,
    onchange,
    compact = false,
    label = "Status",
    triggerID,
  }: {
    value: string;
    config: TaskConfig;
    disabled: boolean;
    onchange: (id: string) => void;
    compact?: boolean;
    label?: string;
    triggerID?: string;
  } = $props();
  const id = $props.id();
  const current = $derived(
    config.statuses.find((status) => status.id === value),
  );
  let open = $state(false);
  let active = $state(0);
  let trigger: HTMLButtonElement;
  let list: HTMLDivElement;

  function close(restoreFocus = false) {
    list?.hidePopover();
    open = false;
    if (restoreFocus) trigger?.focus();
  }
  async function prepareOpen() {
    if (disabled) return;
    active = Math.max(
      0,
      config.statuses.findIndex((status) => status.id === value),
    );
    open = true;
    await tick();
    list?.focus();
  }
  function choose(index: number) {
    const status = config.statuses[index];
    if (!status || disabled) return;
    close(true);
    if (status.id !== value) onchange(status.id);
  }
  async function navigate(event: KeyboardEvent) {
    if (event.key === "Escape") {
      event.preventDefault();
      event.stopPropagation();
      close(true);
      return;
    }
    if (event.key === "Tab") {
      close();
      return;
    }
    if (event.key === "Enter" || event.key === " ") {
      event.preventDefault();
      choose(active);
      return;
    }
    if (event.key === "ArrowDown")
      active = (active + 1) % config.statuses.length;
    else if (event.key === "ArrowUp")
      active = (active + config.statuses.length - 1) % config.statuses.length;
    else if (event.key === "Home") active = 0;
    else if (event.key === "End") active = config.statuses.length - 1;
    else return;
    event.preventDefault();
    await tick();
    list
      ?.querySelector(`#${CSS.escape(id)}-option-${active}`)
      ?.scrollIntoView({ block: "nearest" });
  }
  $effect(() => {
    if (disabled && open) close();
  });
</script>

{#snippet icon(statusID: string)}
  <svg
    class="status-icon"
    class:done={completedStatus(config, statusID)}
    class:started={!entryStatus(config, statusID) &&
      !completedStatus(config, statusID)}
    viewBox="0 0 20 20"
    aria-hidden="true"
  >
    <circle cx="10" cy="10" r="7" />
    {#if completedStatus(config, statusID)}<path d="m6.5 10 2.3 2.3 4.7-4.6" />
    {:else if !entryStatus(config, statusID)}<path
        class="half"
        d="M10 3a7 7 0 0 1 0 14Z"
      />{/if}
  </svg>
{/snippet}

<div class="status-picker">
  <button
    id={triggerID ?? `${id}-trigger`}
    class="status-chip"
    class:compact
    class:completed={completedStatus(config, value)}
    {disabled}
    bind:this={trigger}
    aria-label={`${label}: ${current?.name || "Unknown"}`}
    title={`${label}: ${current?.name || "Unknown"}`}
    aria-haspopup="listbox"
    aria-expanded={open}
    aria-controls={`${id}-list`}
    popovertarget={`${id}-list`}
    onkeydown={(event) => {
      if (event.key === "ArrowDown" || event.key === "ArrowUp") {
        event.preventDefault();
        list.showPopover();
      }
    }}
  >
    {@render icon(value)}{#if !compact}<span>{current?.name || "Unknown"}</span
      >{/if}
    {#if !disabled && !compact}<svg
        class="chevron"
        class:open
        viewBox="0 0 20 20"
        aria-hidden="true"><path d="m6 8 4 4 4-4" /></svg
      >{/if}
  </button>
  <div
    popover="auto"
    use:anchoredPopover={() => ({
      anchor: trigger,
      width: 240,
      height: 320,
      align: "start",
    })}
    onbeforetoggle={(event) => {
      open = event.newState === "open";
      if (open) void prepareOpen();
    }}
    ontoggle={(event) => {
      if (event.newState === "open") list.focus();
    }}
    class="status-options"
    bind:this={list}
    id={`${id}-list`}
    role="listbox"
    aria-label="Task status"
    tabindex="0"
    aria-activedescendant={`${id}-option-${active}`}
    onkeydown={navigate}
  >
    {#each config.statuses as status, index}
      {#if index === 0 || status.board !== config.statuses[index - 1].board}<div
          class="status-group"
          role="presentation"
        >
          {config.boards?.find((b) => b.slug === status.board)?.name ?? "Tasks"}
        </div>{/if}
      <button
        type="button"
        role="option"
        id={`${id}-option-${index}`}
        aria-selected={status.id === value}
        tabindex="-1"
        class:active={active === index}
        onclick={() => choose(index)}
        onpointermove={() => (active = index)}
      >
        {@render icon(status.id)}<span>{status.name}</span>
        {#if status.id === value}<svg
            class="check"
            viewBox="0 0 20 20"
            aria-hidden="true"><path d="m5 10 3 3 7-7" /></svg
          >{/if}
      </button>
    {/each}
  </div>
</div>

<style>
  .status-group {
    padding: 10px 8px 5px;
    font-size: 11px;
    color: var(--muted);
    font-weight: 600;
  }
  .status-picker {
    position: relative;
    min-width: 0;
  }
  .status-chip {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    max-width: 100%;
    min-height: 32px;
    padding: 5px 9px;
    border: 0;
    border-radius: 8px;
    background: color-mix(in srgb, var(--hover-surface) 75%, transparent);
    color: var(--text);
    font-size: 13px;
    font-weight: 500;
    text-align: left;
    transition: background 140ms ease;
  }
  .status-chip:hover:not(:disabled),
  .status-chip[aria-expanded="true"] {
    background: var(--hover-surface);
  }
  .status-chip:disabled {
    cursor: default;
    opacity: 1;
  }
  .status-chip.compact {
    width: 32px;
    padding: 8px;
    background: transparent;
  }
  .status-chip.completed {
    color: var(--success-text);
    background: var(--success-surface);
  }
  .status-chip span,
  .status-options span {
    overflow-wrap: anywhere;
    min-width: 0;
  }
  svg {
    width: 16px;
    height: 16px;
    flex-shrink: 0;
    fill: none;
    stroke: currentColor;
    stroke-width: 1.5;
    stroke-linecap: round;
    stroke-linejoin: round;
  }
  .status-icon {
    color: var(--muted);
  }
  .status-icon.done {
    color: var(--success-text);
  }
  .status-icon.started {
    color: var(--accent);
  }
  .half {
    fill: currentColor;
    stroke: none;
  }
  .chevron {
    width: 13px;
    height: 13px;
    margin-left: 3px;
    color: var(--muted);
    transition: transform 140ms ease;
  }
  .chevron.open {
    transform: rotate(180deg);
  }
  .status-options {
    position: fixed;
    inset: auto;
    margin: 0;
    z-index: 6;

    overflow: auto;
    padding: 5px;
    border: 1px solid var(--panel-border);
    border-radius: 12px;
    background: var(--surface);
    box-shadow: 0 12px 36px #0003;
  }
  .status-options:focus {
    outline: none;
  }
  .status-options button {
    display: flex;
    align-items: center;
    gap: 9px;
    width: 100%;
    min-height: 36px;
    padding: 8px;
    border: 0;
    border-radius: 7px;
    background: transparent;
    color: var(--text);
    font-size: 13px;
    text-align: left;
  }
  .status-options button.active {
    background: var(--hover-surface);
  }
  .check {
    margin-left: auto;
    color: var(--accent);
  }
  @media (prefers-reduced-motion: reduce) {
    .status-chip,
    .chevron {
      transition: none;
    }
  }
</style>
