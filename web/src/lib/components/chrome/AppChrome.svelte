<script lang="ts">
  import { mobileViewport } from "$lib/mobile-viewport";
  import { swipeNavigation } from "$lib/swipe-navigation";
  import { ScopeHistory } from "$lib/scope-history.js";
  import { tick, type Snippet } from "svelte";
  import { goto, afterNavigate } from "$app/navigation";
  import { page } from "$app/state";
  import type { Account } from "$lib/api";
  import { provideNotifications } from "$lib/notifications.svelte";
  import { provideThreads } from "$lib/threads.svelte";
  provideThreads();
  import TaskSearch from "../tasks/TaskSearch.svelte";
  import ThreadTaskViewer from "../tasks/ThreadTaskViewer.svelte";
  let search = $state<TaskSearch>();
  let searchTask = $state(""),
    searchComment = $state(""),
    searchLayout = $state("");
  let viewport = $state(0);
  function openSearch() {
    drawer?.close();
    void search?.open();
  }
  import Sidebar from "./Sidebar.svelte";
  import SidebarResizer from "./SidebarResizer.svelte";
  import NavigationToggle from "./NavigationToggle.svelte";
  import { provideNavigation } from "$lib/navigation-context";
  import { SIDEBAR_DEFAULT, SIDEBAR_RAIL } from "$lib/sidebar-size";
  let {
    account,
    onLogout,
    busy,
    children,
  }: {
    account: Account;
    onLogout: () => void;
    busy: boolean;
    children?: Snippet;
  } = $props();
  provideNotifications(() => account);
  import { useWorkspace } from "$lib/workspaces";
  import { canOpenUsers, checkPermission } from "$lib/permissions";
  const workspace = useWorkspace();
  const scope = $derived(
    page.url.pathname.startsWith("/my-agents")
      ? "agents"
      : page.url.pathname.startsWith("/workspaces")
        ? "workspace"
        : page.url.pathname.startsWith("/user-settings/")
          ? "user"
          : "site",
  );
  let collapsed = $state(false);
  let sidebarWidth = $state(SIDEBAR_DEFAULT);
  const searchAvailable = $derived(
    viewport - (viewport <= 720 ? 0 : collapsed ? SIDEBAR_RAIL : sidebarWidth),
  );
  let resizing = $state(false);
  const sidebarId = $props.id();
  let drawer = $state<HTMLDialogElement>();
  provideNavigation({ open: () => drawer?.showModal() });
  let heading = $state<HTMLHeadingElement>();
  let content = $state<HTMLElement>();
  const taskPage = $derived(/^\/workspaces\/[^/]+\/?$/.test(page.url.pathname));
  const title = $derived(
    scope === "agents"
      ? "My Agents"
      : scope === "workspace"
        ? page.url.pathname.endsWith("/settings/details")
          ? "Workspace details"
          : page.url.pathname.endsWith("/settings/members")
            ? "Members"
            : (workspace.workspace?.name ?? "Workspaces")
        : scope === "site"
          ? page.url.pathname.startsWith("/site-settings/users")
            ? "Users"
            : page.url.pathname.startsWith("/site-settings/groups")
              ? "Groups"
              : page.url.pathname.startsWith("/site-settings/backups")
                ? "Backups"
                : "Site Settings"
          : page.url.pathname === "/user-settings/guide"
            ? "Guide"
            : page.url.pathname === "/user-settings/memories"
              ? "Memories"
              : page.url.pathname === "/user-settings/security"
                ? "Security"
                : page.url.pathname.startsWith("/user-settings/agents")
                  ? "Agents"
                  : page.url.pathname === "/user-settings/harnesses"
                    ? "Harnesses"
                    : "Profile",
  );
  const scopeHistory = new ScopeHistory(() => window.localStorage);
  async function choose(next: "site" | "user" | "workspace" | "agents") {
    scopeHistory.remember(
      account.id,
      page.url.pathname + page.url.search + page.url.hash,
    );
    await goto(
      next === "agents"
        ? scopeHistory.destination(account.id, "agents")
        : next === "workspace"
          ? scopeHistory.destination(account.id, "workspace")
          : next === "site"
            ? canOpenUsers(account)
              ? "/site-settings/users"
              : checkPermission(account, "site.permissions.manage")
                ? "/site-settings/groups"
                : "/site-settings/backups"
            : "/user-settings/profile",
    );
  }
  afterNavigate(async (navigation) => {
    scopeHistory.remember(
      account.id,
      page.url.pathname + page.url.search + page.url.hash,
    );
    drawer?.close();
    if (
      navigation.type !== "enter" &&
      navigation.from?.url.pathname !== navigation.to?.url.pathname
    ) {
      await tick();
      (
        content?.querySelector<HTMLElement>("[autofocus]") ??
        heading ??
        content
      )?.focus({ preventScroll: true });
    }
  });
</script>

<svelte:head><title>{title} · Acta</title></svelte:head>
<svelte:window
  bind:innerWidth={viewport}
  onkeydown={(e) => {
    if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "k") {
      e.preventDefault();
      openSearch();
    }
  }}
  onresize={() => {
    if (window.innerWidth > 720) drawer?.close();
  }}
/>
<div
  use:swipeNavigation={() => drawer}
  use:mobileViewport
  class="app-chrome"
  class:resizing
  style:--sidebar-width={`${collapsed ? SIDEBAR_RAIL : sidebarWidth}px`}
>
  <aside id={sidebarId} class="desktop-sidebar" aria-label="Sidebar">
    <Sidebar
      {account}
      {scope}
      {collapsed}
      onToggle={() => (collapsed = !collapsed)}
      onScope={choose}
      onSearch={openSearch}
      {onLogout}
      {busy}
    />
    <SidebarResizer
      bind:width={sidebarWidth}
      bind:collapsed
      bind:dragging={resizing}
      controls={sidebarId}
    />
  </aside>
  <dialog
    bind:this={drawer}
    class="mobile-sidebar"
    aria-label="Navigation"
    onpointerdown={(event) => {
      if (event.target === drawer) drawer?.close();
    }}
  >
    <div class="drawer-content">
      <Sidebar
        {account}
        {scope}
        mobile
        onToggle={() => drawer?.close()}
        onScope={choose}
        onSearch={openSearch}
        {onLogout}
        {busy}
      />
    </div>
  </dialog>
  <div class="main-column">
    {#if !taskPage && scope !== "agents"}
      <header class="page-header">
        <NavigationToggle />
        <h1 bind:this={heading} tabindex="-1">{title}</h1>
      </header>
    {/if}
    <main bind:this={content} tabindex="-1">
      {#if children}{@render children()}{/if}
    </main>
  </div>
  <div
    class="search-task-pane"
    class:panel={!!searchTask && searchLayout === "panel"}
  >
    <ThreadTaskViewer
      selected={searchTask}
      focusComment={searchComment}
      available={searchAvailable}
      onlayout={(mode) => (searchLayout = mode)}
      onopen={(id) => {
        searchTask = id;
        searchComment = "";
      }}
      onclose={() => {
        searchTask = "";
        searchComment = "";
      }}
    />
  </div>
</div>
<TaskSearch
  bind:this={search}
  onselect={(result) => {
    searchTask = result.id;
    searchComment = result.comment_id || "";
  }}
/>

<style>
  .search-task-pane {
    min-height: 0;
    min-width: 0;
  }
  .search-task-pane.panel {
    padding: 24px 24px 24px 0;
  }

  .app-chrome {
    height: 100dvh;
    overflow: hidden;
    display: grid;
    grid-template-rows: minmax(0, 1fr);
    grid-template-columns: var(--sidebar-width) minmax(0, 1fr) auto;
    transition: grid-template-columns 180ms ease;
  }
  .app-chrome.resizing {
    transition: none;
    user-select: none;
    cursor: col-resize;
  }
  .desktop-sidebar {
    position: relative;
    min-width: 0;
    background: var(--sidebar-surface);
    border-right: 1px solid var(--panel-border);
    min-height: 0;
  }
  .main-column {
    min-height: 0;
    min-width: 0;
    display: flex;
    flex-direction: column;
    background: var(--surface);
  }
  .page-header {
    flex-shrink: 0;
    min-height: 76px;
    padding: 0 32px;
    display: flex;
    align-items: center;
    gap: 14px;
    border-bottom: 1px solid var(--panel-border);
  }
  h1 {
    font-size: 15px;
    line-height: 1.5;
    letter-spacing: -0.1px;
    margin: 0;
    font-weight: 600;
  }
  h1:focus {
    outline: none;
  }
  main:focus {
    outline: none;
  }
  main {
    --content-block-padding: 24px;
    flex: 1;
    min-height: 0;
    overflow: auto;
    overscroll-behavior: contain;
    padding: var(--content-block-padding) 32px;
    display: flex;
    flex-direction: column;
  }
  .mobile-sidebar {
    padding: 0;
    border: 0;
    margin: 0;
    width: 100%;
    max-width: none;
    height: 100dvh;
    max-height: none;
    background: transparent;
    color: var(--text);
    overflow: hidden;
    transition:
      display 200ms allow-discrete,
      overlay 200ms allow-discrete;
  }
  .mobile-sidebar::backdrop {
    background: #00000050;
    opacity: 0;
    transition:
      opacity 200ms ease,
      display 200ms allow-discrete,
      overlay 200ms allow-discrete;
  }
  .mobile-sidebar[open]::backdrop {
    opacity: 1;
  }
  .drawer-content {
    width: min(280px, 85vw);
    height: 100%;
    background: var(--sidebar-surface);
    border-right: 1px solid var(--panel-border);
    transform: translateX(-100%);
    transition: transform 200ms cubic-bezier(0.22, 1, 0.36, 1);
  }
  .mobile-sidebar[open] .drawer-content {
    transform: translateX(0);
  }
  @starting-style {
    .mobile-sidebar[open] .drawer-content {
      transform: translateX(-100%);
    }
    .mobile-sidebar[open]::backdrop {
      opacity: 0;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .app-chrome,
    .mobile-sidebar,
    .mobile-sidebar::backdrop,
    .drawer-content {
      transition: none;
    }
  }
  @media (min-width: 721px) {
    .mobile-sidebar {
      display: none;
    }
  }
  @media (max-width: 720px) {
    .app-chrome {
      position: fixed;
      inset-inline: 0;
      top: var(--mobile-viewport-top, 0px);
      height: var(--mobile-viewport-height, 100dvh);
      grid-template-columns: minmax(0, 1fr);
    }
    .desktop-sidebar {
      display: none;
    }
    .page-header {
      padding: 0 16px;
      min-height: 64px;
      gap: 8px;
    }
    main {
      --content-block-padding: 16px;
      padding: 16px max(16px, env(safe-area-inset-right))
        max(16px, env(safe-area-inset-bottom))
        max(16px, env(safe-area-inset-left));
    }
  }
</style>
