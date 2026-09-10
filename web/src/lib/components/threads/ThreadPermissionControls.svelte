<script lang="ts">
  import { untrack } from "svelte";
  import { anchoredPopover } from "$lib/anchored-popover";
  import {
    permissionModes,
    type ApprovalItem,
    type PermissionState,
  } from "$lib/thread-permissions.js";
  import ThreadInteraction from "./ThreadInteraction.svelte";
  let {
    mode,
    permissionState,
    editable,
    items,
    answer,
    editQuestion = () => {},
    change,
  }: {
    mode: string;
    permissionState: PermissionState;
    editable: boolean;
    items: ApprovalItem[];
    editQuestion?: (
      item: ApprovalItem,
      id: string,
      value: { selected: string[]; text: string },
    ) => void;
    answer: (
      item: ApprovalItem,
      decision: "approve" | "deny" | "answer",
      values?: Record<string, string[]>,
    ) => void;
    change: (mode: string) => void;
  } = $props();
  const id = $props.id();
  let trigger = $state<HTMLButtonElement>();
  let pill = $state<HTMLButtonElement>();
  let popup = $state<HTMLDivElement>();
  let menu = $state<HTMLDivElement>();
  const pending = $derived(items.filter((i) => i.status === "pending"));
  const question = $derived(pending[0]?.frame.kind === "question/request");
  const label = $derived(
    permissionModes.find((m) => m.id === mode)?.label ??
      (mode === "unknown" ? "Permissions unknown" : "Custom permissions"),
  );
  const announced = new Set<string>();
  $effect(() => {
    const ids = pending.map((i) => i.id);
    const node = popup;
    untrack(() => {
      if (!ids.length) {
        node?.hidePopover();
        return;
      }
      const fresh = ids.some((id) => !announced.has(id));
      for (const id of ids) announced.add(id);
      if (fresh) node?.showPopover();
    });
  });
</script>

<button
  class="mode"
  bind:this={trigger}
  popovertarget={id + "-modes"}
  aria-label="Permission mode"
  title={label}
  ><svg
    viewBox="0 0 24 24"
    width="15"
    height="15"
    fill="none"
    stroke="currentColor"
    stroke-width="1.6"
    aria-hidden="true"><path d="m12 3 8 3v6c0 5-8 9-8 9s-8-4-8-9V6z" /></svg
  ><span>{permissionState.modePending ? "Updating…" : label}</span><span
    aria-hidden="true">⌄</span
  ></button
>
<div
  id={id + "-modes"}
  popover
  bind:this={menu}
  use:anchoredPopover={() => ({ anchor: trigger, width: 300, height: 360 })}
  class="permission-menu"
  role="group"
  aria-label="Permission modes"
>
  <div class="menu-title">Permissions</div>
  {#each permissionModes as option}<button
      class:selected={mode === option.id}
      disabled={!editable || !!permissionState.modePending}
      onclick={() => {
        change(option.id);
        menu?.hidePopover();
      }}
      ><strong>{option.label}</strong><span>{option.description}</span
      >{#if mode === option.id}<b aria-hidden="true">✓</b>{/if}</button
    >{/each}
  {#if !editable}<p>
      Available on a connected main thread when no other change is pending.
    </p>{/if}
</div>
{#if permissionState.modeError}<span class="mode-error" role="alert"
    >{permissionState.modeError}</span
  >{/if}
{#if pending.length}<button
    class="approval-pill"
    bind:this={pill}
    popovertarget={id + "-approval"}
    >{question ? "Question to answer" : "Needs approval"}{pending.length > 1
      ? ` (${pending.length})`
      : ""}</button
  >{/if}
<div
  id={id + "-approval"}
  popover
  bind:this={popup}
  class="approval-popup"
  use:anchoredPopover={() => ({
    anchor: pill ?? trigger,
    width: 460,
    height: 560,
    align: "start",
  })}
  aria-label={question ? "Answer requested" : "Approval required"}
>
  <div class="popup-heading">
    <span
      >{question ? "Answer requested" : "Approval required"}{pending.length > 1
        ? ` · ${pending.length} pending`
        : ""}</span
    ><button
      aria-label={question
        ? "Dismiss question popup"
        : "Dismiss approval popup"}
      onclick={() => popup?.hidePopover()}>×</button
    >
  </div>
  {#if pending[0]}<ThreadInteraction
      item={pending[0]}
      {answer}
      {editQuestion}
      compact
    />{/if}
</div>

<style>
  .mode,
  .approval-pill {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    width: auto;
    min-height: 28px;
    padding: 4px 7px;
    border: 0;
    background: transparent;
    color: var(--muted);
    font-size: 11px;
    border-radius: 7px;
    white-space: nowrap;
  }
  .mode:hover {
    background: var(--surface);
    color: var(--text);
  }
  .approval-pill {
    background: var(--surface);
    color: var(--text);
    border: 1px solid var(--panel-border);
  }
  .permission-menu,
  .approval-popup {
    margin: 0;
    border: 1px solid var(--panel-border);
    background: var(--surface);
    color: var(--text);
    border-radius: 14px;
    box-shadow: 0 12px 42px #0005;
    padding: 8px;
    overflow: auto;
  }
  .menu-title {
    font-size: 11px;
    color: var(--muted);
    padding: 7px 10px;
  }
  .permission-menu > button {
    position: relative;
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    text-align: left;
    gap: 4px;
    padding: 10px 30px 10px 10px;
    border: 0;
    background: transparent;
    border-radius: 8px;
    width: 100%;
  }
  .permission-menu > button:hover,
  .permission-menu > button.selected {
    background: var(--hover-surface);
  }
  .permission-menu strong {
    font-size: 12px;
    font-weight: 550;
  }
  .permission-menu button span {
    font-size: 11px;
    color: var(--muted);
    line-height: 1.5;
  }
  .permission-menu b {
    position: absolute;
    right: 10px;
    top: 12px;
    font-size: 12px;
  }
  .permission-menu p {
    font-size: 11px;
    color: var(--muted);
    padding: 0 10px;
  }
  .approval-popup {
    padding: 16px;
  }
  .popup-heading {
    display: flex;
    justify-content: space-between;
    align-items: center;
    color: var(--muted);
    font-size: 11px;
    margin-bottom: 15px;
  }
  .popup-heading button {
    border: 0;
    background: transparent;
    width: 24px;
    min-height: 24px;
    padding: 0;
    font-size: 20px;
    color: var(--muted);
  }
  .mode-error {
    color: var(--danger);
    font-size: 11px;
  }
  @media (max-width: 550px) {
    .mode span:first-of-type {
      max-width: 105px;
      overflow: hidden;
      text-overflow: ellipsis;
    }
  }
</style>
