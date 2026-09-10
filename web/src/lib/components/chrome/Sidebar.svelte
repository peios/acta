<script lang="ts">
  import { useThreads, threadName } from "$lib/threads.svelte";
  const threads = useThreads();
  import { useNotifications } from "$lib/notifications.svelte";
  import NotificationBell from "./NotificationBell.svelte";
  const notifications = useNotifications();
  import { api, errorMessage } from "$lib/api";
  import { taskChanged } from "$lib/tasks";
  let boardError = $state("");
  let boardDrop = $state("");
  let movingTask = $state(false);
  function boardDrag(event: DragEvent, slug: string) {
    if (
      !canWorkspace(workspace.workspace, "tasks.edit") ||
      movingTask ||
      !event.dataTransfer?.types.includes("application/x-acta-task")
    )
      return;
    event.preventDefault();
    boardDrop = slug;
    event.dataTransfer.dropEffect = "move";
  }
  async function dropOnBoard(event: DragEvent, slug: string) {
    event.preventDefault();
    boardDrop = "";
    if (movingTask || !canWorkspace(workspace.workspace, "tasks.edit")) return;
    const raw = event.dataTransfer?.getData("application/x-acta-task");
    if (!raw) return;
    try {
      const task = JSON.parse(raw);
      if (task.workspace_id !== workspace.workspace?.id) return;
      movingTask = true;
      boardError = "";
      await api(`tasks/${task.id}`, {
        field: "board",
        value: slug,
        version: task.version,
      });
      taskChanged();
    } catch (e) {
      boardError = errorMessage(e);
    } finally {
      movingTask = false;
    }
  }
  import { tick } from "svelte";
  import { page } from "$app/state";
  import { canOpenUsers, canOpenSite, checkPermission } from "$lib/permissions";
  import type { Account } from "$lib/api";
  import ThemeToggle from "../ThemeToggle.svelte";
  import { version } from "../../../../package.json";

  import WorkspaceSwitcher from "../workspaces/WorkspaceSwitcher.svelte";
  import { useWorkspace, workspacePath, canWorkspace } from "$lib/workspaces";
  const workspace = useWorkspace();
  let {
    account,
    scope,
    onScope,
    onSearch = () => {},
    onLogout,
    onToggle,
    collapsed = false,
    mobile = false,
    busy = false,
  }: {
    account: Account;
    scope: "site" | "user" | "workspace" | "agents";
    onScope: (scope: "site" | "user" | "workspace" | "agents") => void;
    onSearch?: () => void;
    onLogout: () => void;
    onToggle: () => void;
    collapsed?: boolean;
    mobile?: boolean;
    busy?: boolean;
  } = $props();
  const menuId = $props.id();
  let profileButton = $state<HTMLButtonElement>();
  let menu = $state<HTMLDivElement>();
  let menuOpen = $state(false);
  let menuLeft = $state(0);
  let menuBottom = $state(0);
  let menuWidth = $state(240);
  const name = $derived(account.display_name || account.username);
  const initial = $derived(Array.from(name)[0]?.toLocaleUpperCase() || "?");
  const toggleLabel = $derived(
    mobile
      ? "Close navigation"
      : collapsed
        ? "Expand sidebar"
        : "Collapse sidebar",
  );

  function choose(next: "site" | "user" | "workspace" | "agents") {
    menu?.hidePopover();
    onScope(next);
  }
  function positionMenu(event: Event) {
    if ((event as ToggleEvent).newState !== "open" || !profileButton) return;
    const box = profileButton.getBoundingClientRect();
    menuLeft = box.left;
    menuBottom = window.innerHeight - box.top + 10;
    menuWidth = Math.min(248, window.innerWidth - box.left - 12);
  }
  async function menuToggled(event: Event) {
    menuOpen = (event as ToggleEvent).newState === "open";
    if (menuOpen) {
      await tick();
      menu?.querySelector<HTMLButtonElement>("button")?.focus();
    }
  }
  function menuKeys(event: KeyboardEvent) {
    const items = Array.from(
      menu?.querySelectorAll<HTMLButtonElement>("button:not(:disabled)") ?? [],
    );
    const current = items.indexOf(document.activeElement as HTMLButtonElement);
    if (event.key === "Tab") {
      menu?.hidePopover();
      return;
    }
    const next =
      event.key === "ArrowDown"
        ? (current + 1) % items.length
        : event.key === "ArrowUp"
          ? (current - 1 + items.length) % items.length
          : event.key === "Home"
            ? 0
            : event.key === "End"
              ? items.length - 1
              : -1;
    if (next >= 0) {
      event.preventDefault();
      items[next]?.focus();
    }
  }
</script>

<svelte:window onresize={() => menu?.hidePopover()} />
<div class="sidebar-inner" class:collapsed>
  <div class="sidebar-heading">
    {#if scope === "workspace"}{#if !collapsed}<WorkspaceSwitcher
        />{/if}{:else}<p class="scope-label">
        {scope === "agents"
          ? "My Agents"
          : scope === "site"
            ? "Site settings"
            : "User settings"}
      </p>{/if}
    <button
      class="collapse-toggle"
      type="button"
      aria-label={toggleLabel}
      title={toggleLabel}
      aria-expanded={mobile ? undefined : !collapsed}
      onclick={() => {
        menu?.hidePopover();
        onToggle();
      }}
    >
      <svg
        viewBox="0 0 24 24"
        width="19"
        height="19"
        fill="none"
        stroke="currentColor"
        stroke-width="1.5"
        stroke-linecap="round"
        stroke-linejoin="round"
        aria-hidden="true"
      >
        <rect x="3" y="4" width="18" height="16" rx="3" /><path d="M9 4v16" />
        {#if collapsed}<path d="m13 9 3 3-3 3" />{:else}<path
            d="m16 9-3 3 3 3"
          />{/if}
      </svg>
      <span class="rail-tooltip" aria-hidden="true">{toggleLabel}</span>
    </button>
  </div>
  <nav
    class="scope-navigation"
    aria-label={scope === "workspace"
      ? "Workspace navigation"
      : scope === "site"
        ? "Site settings navigation"
        : "User settings navigation"}
  >
    <button
      class="scope-button search-button"
      aria-label="Search"
      title="Search (Ctrl/Cmd+K)"
      onclick={onSearch}
      ><svg
        width="19"
        height="19"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        stroke-width="1.6"
        aria-hidden="true"
        ><circle cx="10.5" cy="10.5" r="6.5" /><path d="m16 16 5 5" /></svg
      ><span class="scope-name">Search</span><span
        class="rail-tooltip"
        aria-hidden="true">Search</span
      ></button
    >
    {#if scope === "agents"}
      <button
        class="scope-button"
        aria-label="New thread"
        onclick={() => (threads.createOpen = true)}
        ><span aria-hidden="true">＋</span><span class="scope-name"
          >New thread</span
        ><span class="rail-tooltip" aria-hidden="true">New thread</span></button
      >
      {#each threads.items as thread (thread.id)}<a
          class="scope-button"
          class:active={page.url.pathname === `/my-agents/${thread.id}`}
          href={`/my-agents/${thread.id}`}
          aria-label={threadName(thread)}
          title={thread.cwd}
          ><svg
            width="19"
            height="19"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="1.5"
            aria-hidden="true"><path d="M4 4h16v12H9l-5 4z" /></svg
          ><span class="scope-name">{threadName(thread)}</span
          >{#if notifications?.inbox.counts[thread.id]}<span
              class="unread-dot"
              title="Unread notifications"
              aria-label="Unread notifications"
            ></span>{/if}<span class="rail-tooltip" aria-hidden="true"
            >{threadName(thread)}</span
          ></a
        >{/each}
    {:else if scope === "user"}
      <a
        href="/user-settings/profile"
        class="scope-button"
        class:active={page.url.pathname === "/user-settings/profile"}
        aria-current={page.url.pathname === "/user-settings/profile"
          ? "page"
          : undefined}
        aria-label="Profile"
      >
        <svg
          viewBox="0 0 24 24"
          width="19"
          height="19"
          fill="none"
          stroke="currentColor"
          stroke-width="1.6"
          stroke-linecap="round"
          aria-hidden="true"
          ><circle cx="12" cy="8" r="3.5" /><path
            d="M5 20v-2a7 7 0 0 1 14 0v2"
          /></svg
        >
        <span class="scope-name">Profile</span><span
          class="rail-tooltip"
          aria-hidden="true">Profile</span
        >
      </a>
      <a
        href="/user-settings/security"
        class="scope-button"
        class:active={page.url.pathname === "/user-settings/security"}
        aria-current={page.url.pathname === "/user-settings/security"
          ? "page"
          : undefined}
        aria-label="Security"
      >
        <svg
          viewBox="0 0 24 24"
          width="19"
          height="19"
          fill="none"
          stroke="currentColor"
          stroke-width="1.6"
          stroke-linecap="round"
          aria-hidden="true"
          ><path d="m12 3 8 3v6c0 5-8 9-8 9s-8-4-8-9V6z" /><path
            d="m8 12 3 3 5-6"
          /></svg
        ><span class="scope-name">Security</span><span
          class="rail-tooltip"
          aria-hidden="true">Security</span
        >
      </a>
      <a
        href="/user-settings/agents"
        class="scope-button"
        class:active={page.url.pathname.startsWith("/user-settings/agents")}
        aria-current={page.url.pathname.startsWith("/user-settings/agents")
          ? "page"
          : undefined}
        aria-label="Agents"
      >
        <svg
          viewBox="0 0 24 24"
          width="19"
          height="19"
          fill="none"
          stroke="currentColor"
          stroke-width="1.6"
          stroke-linecap="round"
          aria-hidden="true"
          ><rect x="4" y="7" width="16" height="13" rx="4" /><path
            d="M12 3v4M8 12v2m8-2v2M9 17h6M2 11v5m20-5v5"
          /></svg
        >
        <span class="scope-name">Agents</span><span
          class="rail-tooltip"
          aria-hidden="true">Agents</span
        >
      </a>
      <a
        href="/user-settings/guide"
        class="scope-button"
        class:active={page.url.pathname === "/user-settings/guide"}
        aria-current={page.url.pathname === "/user-settings/guide"
          ? "page"
          : undefined}
        aria-label="Guide"
      >
        <svg
          width="19"
          height="19"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="1.6"
          aria-hidden="true"
          ><path
            d="M12 5v16M12 5C8 2 4 3 2 4v15c4-2 7-1 10 2 3-3 6-4 10-2V4c-2-1-6-2-10 1Z"
          /></svg
        ><span class="scope-name">Guide</span><span
          class="rail-tooltip"
          aria-hidden="true">Guide</span
        >
      </a>
      <a
        href="/user-settings/memories"
        class="scope-button"
        class:active={page.url.pathname === "/user-settings/memories"}
        aria-label="Memories"
      >
        <svg
          width="19"
          height="19"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="1.6"
          aria-hidden="true"><path d="M5 4h14v16H5zM8 8h8M8 12h8M8 16h5" /></svg
        ><span class="scope-name">Memories</span><span
          class="rail-tooltip"
          aria-hidden="true">Memories</span
        >
      </a>
      <a
        href="/user-settings/harnesses"
        class="scope-button"
        class:active={page.url.pathname === "/user-settings/harnesses"}
        aria-current={page.url.pathname === "/user-settings/harnesses"
          ? "page"
          : undefined}
        aria-label="Harnesses"
      >
        <svg
          viewBox="0 0 24 24"
          width="19"
          height="19"
          fill="none"
          stroke="currentColor"
          stroke-width="1.6"
          stroke-linecap="round"
          aria-hidden="true"
          ><rect x="3" y="3" width="18" height="13" rx="2" /><path
            d="M8 21h8m-4-5v5"
          /></svg
        >
        <span class="scope-name">Harnesses</span><span
          class="rail-tooltip"
          aria-hidden="true">Harnesses</span
        >
      </a>
    {:else if scope === "workspace"}
      {#if workspace.workspace}
        {@const w = workspace.workspace}
        {#each [{ slug: "tasks", name: "Tasks" }, { slug: "backlog", name: "Backlog" }] as board}
          <a
            href={workspacePath(w) +
              (board.slug === "backlog" ? "?board=backlog" : "")}
            ondragover={(event) => boardDrag(event, board.slug)}
            ondragleave={() => (boardDrop = "")}
            ondrop={(event) => void dropOnBoard(event, board.slug)}
            class:drop-target={boardDrop === board.slug}
            class="scope-button"
            class:active={page.url.pathname === workspacePath(w) &&
              (page.url.searchParams.get("board") === "backlog"
                ? "backlog"
                : "tasks") === board.slug}
            aria-current={page.url.pathname === workspacePath(w) &&
            (page.url.searchParams.get("board") === "backlog"
              ? "backlog"
              : "tasks") === board.slug
              ? "page"
              : undefined}
            aria-label={board.name}
            ><svg
              width="19"
              height="19"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="1.6"
              aria-hidden="true"
              ><rect x="3" y="3" width="18" height="18" rx="3" /><path
                d="M3 9h18"
              /></svg
            ><span class="scope-name">{board.name}</span><span
              class="rail-tooltip"
              aria-hidden="true">{board.name}</span
            ></a
          >
        {/each}
        {#if boardError}<p class="notice error" role="alert">
            {boardError}
          </p>{/if}
        {#if canWorkspace(w, "workspace.edit") || canWorkspace(w, "tasks.statuses.manage") || canWorkspace(w, "workspace.members.manage") || canWorkspace(w, "workspace.permissions.manage")}
          {#if !collapsed}<p class="workspace-section">
              Workspace settings
            </p>{/if}
          {#each [{ name: "Details", path: "details", show: canWorkspace(w, "workspace.edit") || canWorkspace(w, "tasks.statuses.manage") }, { name: "Members", path: "members", show: canWorkspace(w, "workspace.members.manage") || canWorkspace(w, "workspace.permissions.manage") }] as item}
            {#if item.show}<a
                href={workspacePath(w) + "/settings/" + item.path}
                class="scope-button"
                class:active={page.url.pathname.endsWith(
                  "/settings/" + item.path,
                )}
                aria-current={page.url.pathname.endsWith(
                  "/settings/" + item.path,
                )
                  ? "page"
                  : undefined}
                aria-label={item.name}
                ><svg
                  width="19"
                  height="19"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="1.6"
                  aria-hidden="true"
                  >{#if item.path === "members"}<circle
                      cx="9"
                      cy="8"
                      r="3"
                    /><path
                      d="M3 21v-3a6 6 0 0 1 12 0v3M16 4a4 4 0 0 1 0 8m2 3a5 5 0 0 1 3 5"
                    />{:else}<path d="M4 7h16M4 17h16" /><circle
                      cx="9"
                      cy="7"
                      r="2"
                    /><circle cx="15" cy="17" r="2" />{/if}</svg
                ><span class="scope-name">{item.name}</span><span
                  class="rail-tooltip"
                  aria-hidden="true">{item.name}</span
                ></a
              >{/if}
          {/each}{/if}
      {/if}
    {:else}
      {#if checkPermission(account, "site.backups.manage")}
        <a
          href="/site-settings/backups"
          class="scope-button"
          class:active={page.url.pathname === "/site-settings/backups"}
          aria-current={page.url.pathname === "/site-settings/backups"
            ? "page"
            : undefined}
          aria-label="Backups"
          ><svg
            width="19"
            height="19"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="1.6"
            aria-hidden="true"
            ><path
              d="M4 7h16v13H4zM3 3h18v4H3zM9 11h6M12 14v4m-2-2 2 2 2-2"
            /></svg
          ><span class="scope-name">Backups</span><span
            class="rail-tooltip"
            aria-hidden="true">Backups</span
          ></a
        >
      {/if}
      {#if canOpenUsers(account)}
        <a
          href="/site-settings/users"
          class="scope-button"
          class:active={page.url.pathname.startsWith("/site-settings/users")}
          aria-current={page.url.pathname.startsWith("/site-settings/users")
            ? "page"
            : undefined}
          aria-label="Users"
        >
          <svg
            viewBox="0 0 24 24"
            width="19"
            height="19"
            fill="none"
            stroke="currentColor"
            stroke-width="1.6"
            aria-hidden="true"
            ><circle cx="9" cy="8" r="3" /><path
              d="M3 20v-2a6 6 0 0 1 12 0v2m1-15a3 3 0 0 1 0 6m2 3a5 5 0 0 1 3 4v2"
            /></svg
          ><span class="scope-name">Users</span><span
            class="rail-tooltip"
            aria-hidden="true">Users</span
          >
        </a>
      {/if}
      {#if checkPermission(account, "site.permissions.manage")}
        <a
          href="/site-settings/groups"
          class="scope-button"
          class:active={page.url.pathname.startsWith("/site-settings/groups")}
          aria-current={page.url.pathname.startsWith("/site-settings/groups")
            ? "page"
            : undefined}
          aria-label="Groups"
        >
          <svg
            viewBox="0 0 24 24"
            width="19"
            height="19"
            fill="none"
            stroke="currentColor"
            stroke-width="1.6"
            aria-hidden="true"
            ><circle cx="9" cy="8" r="3" /><circle cx="18" cy="9" r="2" /><path
              d="M3 20v-2a6 6 0 0 1 12 0v2m2-6a4 4 0 0 1 4 4v2"
            /></svg
          ><span class="scope-name">Groups</span><span
            class="rail-tooltip"
            aria-hidden="true">Groups</span
          >
        </a>
      {/if}
    {/if}
  </nav>
  <div class="sidebar-bottom">
    <NotificationBell {collapsed} />
    <div
      class="branding"
      aria-label={`Acta version ${version}`}
      title={`Acta v${version}`}
    >
      <span class="brand" aria-hidden="true">acta<span>.</span></span>
      <span class="version" aria-hidden="true">v{version}</span>
    </div>
    <nav aria-label="Acta scopes" class="scope-picker">
      <button
        class="scope-button"
        class:active={scope === "workspace"}
        onclick={() => choose("workspace")}
        aria-label="Workspaces"
        ><svg
          width="19"
          height="19"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="1.6"
          aria-hidden="true"
          ><rect x="3" y="4" width="18" height="16" rx="3" /><path
            d="M8 4v16m8-16v16"
          /></svg
        ><span class="scope-name">Workspaces</span><span
          class="rail-tooltip"
          aria-hidden="true">Workspaces</span
        ></button
      >
      <button
        class="scope-button"
        class:active={scope === "agents"}
        aria-label="My Agents"
        onclick={() => choose("agents")}
        ><svg
          width="19"
          height="19"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="1.6"
          aria-hidden="true"
          ><rect x="4" y="7" width="16" height="13" rx="4" /><path
            d="M12 3v4M8 12v2m8-2v2M9 17h6"
          /></svg
        ><span class="scope-name">My Agents</span
        >{#if notifications?.inbox.total}<span
            class="unread-dot"
            title="Unread notifications"
            aria-label="Unread notifications"
          ></span>{/if}<span class="rail-tooltip" aria-hidden="true"
          >My Agents</span
        ></button
      >
      {#if canOpenSite(account)}
        <button
          class="scope-button"
          class:active={scope === "site"}
          aria-label="Site Settings"
          aria-current={scope === "site" ? "page" : undefined}
          onclick={() => choose("site")}
        >
          <svg
            viewBox="0 0 24 24"
            width="19"
            height="19"
            fill="none"
            stroke="currentColor"
            stroke-width="1.6"
            stroke-linecap="round"
            aria-hidden="true"
            ><path d="M4 7h16M4 17h16" /><circle
              cx="9"
              cy="7"
              r="2.5"
              fill="var(--sidebar-surface)"
            /><circle
              cx="15"
              cy="17"
              r="2.5"
              fill="var(--sidebar-surface)"
            /></svg
          >
          <span class="scope-name">Site Settings</span>
          <span class="rail-tooltip" aria-hidden="true">Site Settings</span>
        </button>
      {/if}
    </nav>
    <div class="profile-row">
      <button
        class="profile-button"
        class:open={menuOpen}
        bind:this={profileButton}
        popovertarget={menuId}
        aria-label={`Account menu for ${name}`}
        aria-haspopup="menu"
        aria-expanded={menuOpen}
      >
        <span class="avatar" aria-hidden="true">{initial}</span>
        <span class="name">{name}</span>
        <span class="rail-tooltip" aria-hidden="true">{name}</span>
        <svg
          class="chevron"
          class:up={menuOpen}
          viewBox="0 0 16 16"
          width="16"
          height="16"
          fill="none"
          stroke="currentColor"
          stroke-width="1.5"
          aria-hidden="true"><path d="m5 9 3-3 3 3" /></svg
        >
      </button>
      {#if !collapsed}<ThemeToggle placement="inline" />{/if}
    </div>
  </div>
</div>

<div
  bind:this={menu}
  id={menuId}
  popover="auto"
  class="account-menu"
  role="menu"
  aria-label="Account"
  tabindex="-1"
  style:left={`${menuLeft}px`}
  style:bottom={`${menuBottom}px`}
  style:width={`${menuWidth}px`}
  onbeforetoggle={positionMenu}
  ontoggle={menuToggled}
  onkeydown={menuKeys}
>
  <button
    role="menuitem"
    class:selected={scope === "user"}
    onclick={() => choose("user")}
  >
    <svg
      viewBox="0 0 24 24"
      width="18"
      height="18"
      fill="none"
      stroke="currentColor"
      stroke-width="1.6"
      stroke-linecap="round"
      aria-hidden="true"
      ><circle cx="12" cy="8" r="3.5" /><path
        d="M5 20v-2a7 7 0 0 1 14 0v2"
      /></svg
    >
    User Settings
  </button>
  <div class="menu-divider" role="separator"></div>
  <button role="menuitem" disabled={busy} onclick={onLogout}>
    <svg
      viewBox="0 0 24 24"
      width="18"
      height="18"
      fill="none"
      stroke="currentColor"
      stroke-width="1.6"
      stroke-linecap="round"
      stroke-linejoin="round"
      aria-hidden="true"><path d="M10 4H5v16h5m-1-8h12m-4-4 4 4-4 4" /></svg
    >
    {busy ? "Logging out…" : "Log out"}
  </button>
</div>

<style>
  .scope-button.drop-target {
    outline: 2px solid var(--accent);
    background: var(--hover-surface);
  }
  .unread-dot {
    display: block;
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--accent);
    flex-shrink: 0;
  }
  .collapsed .unread-dot {
    position: absolute;
    top: 5px;
    right: 6px;
  }
  .workspace-section {
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.07em;
    color: var(--muted);
    padding: 16px 12px 0;
  }
  .sidebar-inner {
    height: 100%;
    display: flex;
    flex-direction: column;
    min-height: 300px;
  }
  .sidebar-heading {
    min-height: 76px;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    padding: 0 12px 0 24px;
  }
  .collapse-toggle {
    position: relative;
    display: grid;
    place-items: center;
    flex: 0 0 40px;
    width: 40px;
    height: 40px;
    border: 0;
    border-radius: 8px;
    color: var(--muted);
    background: transparent;
  }
  .collapse-toggle:hover {
    color: var(--text);
    background: var(--hover-surface);
  }
  .branding {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 8px;
    padding: 12px 12px 16px;
    color: var(--muted);
  }
  .brand {
    font-size: 19px;
    font-weight: 750;
    letter-spacing: -0.8px;
    line-height: 1;
  }
  .brand span {
    color: var(--accent);
  }
  .version {
    font-size: 10px;
    white-space: nowrap;
    font-variant-numeric: tabular-nums;
  }
  .scope-navigation {
    flex: 1;
    padding: 0 12px;
  }
  .scope-label {
    margin: 0;
    font-size: 11px;
    font-weight: 650;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--muted);
  }
  .sidebar-bottom {
    margin-top: 32px;
    padding: 0 12px max(12px, env(safe-area-inset-bottom));
  }
  .scope-picker {
    border-top: 1px solid var(--panel-border);
    padding: 12px 0 6px;
  }
  .scope-button {
    width: 100%;
    min-height: 42px;
    display: flex;
    align-items: center;
    gap: 11px;
    border: 0;
    padding: 10px 12px;
    border-radius: 8px;
    background: transparent;
    color: var(--muted);
    font-size: 14px;
    text-align: left;
    text-decoration: none;
    transition:
      background-color 150ms,
      color 150ms;
  }
  .scope-button:hover {
    background: var(--hover-surface);
    color: var(--text);
  }
  .scope-button.active {
    color: var(--accent);
    background: var(--scope-active);
    font-weight: 550;
  }
  .search-button {
    margin-bottom: 10px;
    border: 1px solid var(--panel-border);
    background: var(--surface);
    color: var(--text);
  }
  .profile-row {
    display: flex;
    align-items: center;
    gap: 3px;
  }
  .profile-button {
    min-width: 0;
    flex: 1;
    min-height: 44px;
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 6px;
    background: transparent;
    color: var(--text);
    border: 0;
    border-radius: 8px;
    text-align: left;
    transition: background-color 150ms;
  }
  .profile-button:hover,
  .profile-button.open {
    background: var(--hover-surface);
  }
  .avatar {
    width: 30px;
    height: 30px;
    flex: 0 0 30px;
    border-radius: 50%;
    background: var(--scope-active);
    color: var(--accent);
    display: grid;
    place-items: center;
    font-weight: 600;
    font-size: 14px;
  }
  .name {
    min-width: 0;
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: 13px;
    font-weight: 600;
  }
  .chevron {
    color: var(--muted);
    flex-shrink: 0;
    transition: transform 180ms ease;
  }
  .chevron.up {
    transform: rotate(180deg);
  }
  .scope-button,
  .profile-button {
    position: relative;
  }
  .rail-tooltip {
    display: none;
    position: absolute;
    z-index: 20;
    left: calc(100% + 12px);
    top: 50%;
    transform: translateY(-50%);
    padding: 7px 10px;
    border: 1px solid var(--panel-border);
    border-radius: 6px;
    background: var(--surface);
    color: var(--text);
    box-shadow: var(--shadow);
    font-size: 12px;
    font-weight: 400;
    white-space: nowrap;
    pointer-events: none;
  }
  .collapsed .sidebar-heading {
    padding: 0;
    justify-content: center;
  }
  .collapsed .scope-label,
  .collapsed .scope-name,
  .collapsed .name,
  .collapsed .chevron {
    display: none;
  }
  .collapsed .scope-navigation {
    padding: 0 12px;
  }
  .collapsed .branding {
    align-items: center;
    flex-direction: column;
    gap: 5px;
    padding: 12px 0 16px;
  }
  .collapsed .brand {
    font-size: 17px;
  }
  .collapsed .version {
    font-size: 9px;
  }
  .collapsed .scope-button {
    justify-content: center;
    padding: 10px;
  }
  .collapsed .profile-row {
    flex-direction: column;
    gap: 2px;
  }
  .collapsed .profile-button {
    flex: initial;
    width: 44px;
    justify-content: center;
  }
  .collapsed :is(button, a):hover > .rail-tooltip,
  .collapsed :is(button, a):focus-visible > .rail-tooltip {
    display: block;
  }
  .collapsed .profile-button.open > .rail-tooltip {
    display: none;
  }
  .account-menu {
    position: fixed;
    top: auto;
    right: auto;
    margin: 0;
    padding: 6px;
    border: 1px solid var(--panel-border);
    border-radius: 11px;
    background: var(--surface);
    color: var(--text);
    box-shadow: 0 8px 30px #00000020;
  }
  .account-menu:popover-open {
    animation: menu-appear 140ms ease-out;
  }
  .account-menu button {
    width: 100%;
    min-height: 42px;
    display: flex;
    align-items: center;
    gap: 11px;
    padding: 10px;
    border: 0;
    border-radius: 6px;
    background: transparent;
    color: inherit;
    text-align: left;
    font-size: 13px;
  }
  .account-menu button:hover,
  .account-menu button:focus-visible {
    background: var(--hover-surface);
  }
  .account-menu button.selected {
    color: var(--accent);
  }
  .menu-divider {
    height: 1px;
    background: var(--panel-border);
    margin: 5px 4px;
  }
  @keyframes menu-appear {
    from {
      opacity: 0;
      transform: translateY(5px);
    }
    to {
      opacity: 1;
      transform: translateY(0);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .account-menu:popover-open {
      animation: none;
    }
  }
</style>
