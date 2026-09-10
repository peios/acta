<script lang="ts">
  import { tick } from "svelte";
  import NavigationToggle from "../chrome/NavigationToggle.svelte";
  let {
    title = "Tasks",
    archived = false,
    search = $bindable(""),
    width,
    taskOpen,
    canCreate,
    oncreate,
  }: {
    title?: string;
    archived?: boolean;
    search?: string;
    width: number;
    taskOpen: boolean;
    canCreate: boolean;
    oncreate: () => void;
  } = $props();
  let query = $state("");
  $effect(() => {
    void archived;
    query = search;
  });
  let searchExpanded = $state(false);
  let searchInput = $state<HTMLInputElement>();
  const compactSearch = $derived(width <= 620);
  const searchID = $props.id();
  async function submitSearch(event: SubmitEvent) {
    event.preventDefault();
    if (compactSearch && !searchExpanded) {
      searchExpanded = true;
      await tick();
      searchInput?.focus();
      return;
    }
    search = query;
  }
</script>

<div class="list-heading" class:search-expanded={searchExpanded}>
  <div class="heading-title">
    {#if !taskOpen}<NavigationToggle />{/if}
    <h1>{archived ? `Archived ${title.toLowerCase()}` : title}</h1>
  </div>
  <div class="heading-actions">
    <form
      class="task-search"
      class:expanded={searchExpanded}
      role="search"
      onsubmit={submitSearch}
      onfocusout={(event) => {
        if (!event.currentTarget.contains(event.relatedTarget as Node | null))
          searchExpanded = false;
      }}
    >
      <input
        id={searchID}
        bind:this={searchInput}
        type="search"
        aria-label="Search task titles or references"
        placeholder="Search tasks"
        bind:value={query}
        onkeydown={(event) => {
          if (event.key === "Escape" && compactSearch) {
            event.preventDefault();
            event.currentTarget.form?.querySelector("button")?.focus();
            searchExpanded = false;
          }
        }}
      />
      <button
        class="search-button"
        class:active={!!search}
        aria-label="Search tasks"
        aria-expanded={compactSearch ? searchExpanded : undefined}
        aria-controls={searchID}
      >
        <svg
          viewBox="0 0 24 24"
          width="20"
          height="20"
          fill="none"
          stroke="currentColor"
          stroke-width="1.6"
          stroke-linecap="round"
          aria-hidden="true"
        >
          <circle cx="10.5" cy="10.5" r="6.5" /><path d="m16 16 4.5 4.5" />
        </svg>
      </button>
    </form>
    {#if canCreate}
      <button
        class="primary create-task"
        aria-label="Create task"
        title="Create task"
        onclick={oncreate}
      >
        <svg
          viewBox="0 0 24 24"
          width="20"
          height="20"
          fill="none"
          stroke="currentColor"
          stroke-width="1.8"
          stroke-linecap="round"
          aria-hidden="true"><path d="M12 5v14M5 12h14" /></svg
        >
      </button>
    {/if}
  </div>
</div>

<style>
  .list-heading {
    display: flex;
    justify-content: space-between;
    gap: 16px;
    align-items: center;
    margin: 0 0 28px;
  }
  .list-heading .create-task {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 44px;
    height: 44px;
    padding: 0;
    border-radius: 12px;
    flex-shrink: 0;
  }
  .list-heading h1 {
    font-size: 25px;
    margin: 0;
  }
  .heading-title {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .heading-actions {
    display: flex;
    align-items: center;
    gap: 12px;
    min-width: 0;
  }
  .task-search {
    position: relative;
    display: flex;
    align-items: center;
    border-radius: 12px;
    background: var(--hover-surface);
    transition:
      box-shadow 150ms ease,
      background 150ms ease;
  }
  .task-search:focus-within {
    box-shadow: 0 0 0 2px color-mix(in srgb, var(--muted) 35%, transparent);
  }
  .task-search input {
    min-width: 0;
    width: 200px;
    height: 44px;
    padding: 0 0 0 14px;
    border: 0;
    background: transparent;
    font-size: 13px;
    border-radius: 12px 0 0 12px;
  }
  .task-search input:focus {
    outline: none;
    box-shadow: none;
  }
  .search-button {
    flex-shrink: 0;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 44px;
    height: 44px;
    padding: 0;
    border: 0;
    border-radius: 12px;
    background: transparent;
    color: var(--muted);
  }
  .search-button:hover,
  .search-button.active {
    background: var(--hover-surface);
    color: var(--text);
  }
  @container tasks-list (max-width: 620px) {
    .task-search:not(.expanded) {
      background: transparent;
    }
    .task-search input {
      display: none;
    }
    .task-search.expanded input {
      display: block;
      width: min(200px, calc(100cqw - 184px));
    }
  }

  @media (max-width: 759px) {
    .list-heading {
      align-items: start;
    }
  }
  @media (max-width: 720px) {
    .list-heading {
      margin-left: -16px;
    }
    .task-search.expanded input {
      width: clamp(40px, calc(100cqw - 240px), 200px);
    }
  }
  @media (max-width: 440px) {
    .search-expanded .heading-title h1 {
      position: absolute;
      width: 1px;
      height: 1px;
      overflow: hidden;
      clip-path: inset(50%);
    }
    .task-search.expanded input {
      width: calc(100cqw - 160px);
    }
  }
</style>
