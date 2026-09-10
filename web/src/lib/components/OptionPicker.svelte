<script lang="ts">
  import { anchoredPopover } from "$lib/anchored-popover";
  import { tick } from "svelte";
  let {
    label,
    value = "",
    options,
    onchange,
    iconOnly = false,
    disabled = false,
  }: {
    label: string;
    value?: string;
    options: { value: string; label: string }[];
    onchange: (value: string) => void;
    iconOnly?: boolean;
    disabled?: boolean;
  } = $props();
  const id = $props.id();
  const selected = $derived(options.find((option) => option.value === value));
  let trigger: HTMLButtonElement, list: HTMLDivElement;
  let open = $state(false),
    active = $state(0);
  let typed = "",
    typedAt = 0;

  function close(focus = false) {
    list?.hidePopover();
    open = false;
    if (focus) trigger?.focus();
  }
  async function prepareOpen() {
    if (disabled || !options.length) return;
    active = Math.max(
      0,
      options.findIndex((option) => option.value === value),
    );
    typed = "";
    open = true;
    await tick();
    list.focus();
  }
  function choose(index: number) {
    const option = options[index];
    if (!option) return;
    close(true);
    onchange(option.value);
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
    if (event.key === "ArrowDown") active = (active + 1) % options.length;
    else if (event.key === "ArrowUp")
      active = (active + options.length - 1) % options.length;
    else if (event.key === "Home") active = 0;
    else if (event.key === "End") active = options.length - 1;
    else if (
      event.key.length === 1 &&
      !event.ctrlKey &&
      !event.metaKey &&
      !event.altKey
    ) {
      const now = Date.now();
      typed = (now - typedAt < 700 ? typed : "") + event.key.toLowerCase();
      typedAt = now;
      const index = options.findIndex((option) =>
        option.label.toLowerCase().startsWith(typed),
      );
      if (index >= 0) active = index;
    } else return;
    event.preventDefault();
    await tick();
    list.children[active]?.scrollIntoView({ block: "nearest" });
  }
</script>

<button
  class="picker"
  class:icon-only={iconOnly}
  class:open
  bind:this={trigger}
  aria-label={iconOnly ? label : `${label}: ${selected?.label ?? value}`}
  title={label}
  aria-haspopup="listbox"
  aria-controls={id}
  aria-expanded={open}
  disabled={disabled || !options.length}
  popovertarget={id}
  onkeydown={(e) => {
    if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      list.showPopover();
    }
  }}
>
  {#if iconOnly}<svg viewBox="0 0 20 20" aria-hidden="true"
      ><path d="M10 4v12M4 10h12" /></svg
    >
  {:else}<span>{selected?.label ?? value}</span><svg
      class="chevron"
      viewBox="0 0 20 20"
      aria-hidden="true"><path d="m6 8 4 4 4-4" /></svg
    >{/if}
</button>
<div
  bind:this={list}
  {id}
  class="options"
  popover="auto"
  use:anchoredPopover={() => ({
    anchor: trigger,
    width: Math.max(190, trigger?.getBoundingClientRect().width ?? 0),
    height: 280,
    align: "start",
  })}
  role="listbox"
  aria-label={label}
  tabindex="0"
  aria-activedescendant={options[active] ? `${id}-${active}` : undefined}
  onkeydown={navigate}
  onbeforetoggle={(event) => {
    open = event.newState === "open";
    if (open) void prepareOpen();
  }}
>
  {#each options as option, index (option.value)}
    <button
      id={`${id}-${index}`}
      role="option"
      tabindex="-1"
      aria-selected={option.value === value}
      class:highlighted={index === active}
      onpointermove={() => (active = index)}
      onclick={() => choose(index)}
    >
      <span>{option.label}</span>{#if option.value === value}<svg
          viewBox="0 0 20 20"
          aria-hidden="true"><path d="m4 10 4 4 8-8" /></svg
        >{/if}
    </button>
  {/each}
</div>

<style>
  .picker {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    width: 100%;
    min-width: 0;
    min-height: 34px;
    padding: 7px 10px;
    border: 1px solid transparent;
    border-radius: 6px;
    background: var(--hover-surface);
    color: var(--text);
    font-size: 12px;
    text-align: left;
  }
  .picker:hover:not(:disabled),
  .picker.open {
    border-color: var(--panel-border);
  }
  .picker span {
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
  }
  svg {
    width: 14px;
    height: 14px;
    flex-shrink: 0;
    fill: none;
    stroke: currentColor;
    stroke-width: 1.5;
    stroke-linecap: round;
    stroke-linejoin: round;
  }
  .chevron {
    color: var(--muted);
  }
  .picker.icon-only {
    width: 28px;
    min-height: 28px;
    padding: 5px;
    justify-content: center;
    background: transparent;
    color: var(--muted);
  }
  .picker.icon-only:hover:not(:disabled) {
    color: var(--text);
    background: var(--hover-surface);
  }
  .options {
    position: fixed;
    inset: auto;
    margin: 0;
    padding: 5px;
    overflow: auto;
    border: 1px solid var(--panel-border);
    border-radius: 9px;
    background: var(--surface);
    color: var(--text);
    box-shadow: 0 8px 28px #0003;
    outline: none;
  }
  .options button {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    width: 100%;
    border: 0;
    border-radius: 5px;
    background: transparent;
    color: var(--text);
    padding: 9px 10px;
    font-size: 12px;
    text-align: left;
  }
  .options button.highlighted {
    background: var(--hover-surface);
  }
  .options svg {
    color: var(--accent);
  }
  .picker:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }
  .options:focus-visible {
    border-color: var(--accent);
  }
</style>
