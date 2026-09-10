<script lang="ts">
  import { tick } from "svelte";
  import { api, APIError, errorMessage, type Account } from "$lib/api";
  import { useAccount } from "$lib/account-context";
  import { checkPermission, type Permission } from "$lib/permissions";
  import type { Group } from "$lib/groups";
  import type { ScopedPermissions } from "$lib/workspaces";
  let scoped = $state<ScopedPermissions | null>(null);
  let inherit = $state(false);
  function canGrant(id: string) {
    return scoped
      ? scoped.ceiling.includes(id)
      : checkPermission(current.account, id);
  }
  type Subject = {
    id: string;
    label: string;
    kind: "user" | "group" | "agent" | "workspace";
    direct_permissions: string[];
    direct_require_mfa: boolean;
    permissions_version: number;
    groups: Group[];
    last_active_superuser: boolean;
    member_count?: number;
  };
  const current = useAccount();
  let {
    onSaved = () => {},
    onGroupSaved = () => {},
  }: {
    onSaved?: (account: Account) => void;
    onGroupSaved?: (group: Group) => void;
  } = $props();
  let dialog = $state<HTMLDialogElement>(),
    target = $state<Subject | null>(null),
    catalogue = $state<Permission[]>([]),
    grants = $state<string[]>([]),
    requireMFA = $state(false),
    category = $state("Own Account"),
    error = $state(""),
    busy = $state(false),
    loading = $state(false),
    stale = $state(false);
  const id = $props.id();
  const categories = $derived([...new Set(catalogue.map((p) => p.category))]);
  const superuser = $derived(grants.includes("site.superuser"));
  const dirty = $derived(
    !!target &&
      (inherit !== (scoped?.inherit ?? false) ||
        requireMFA !== target.direct_require_mfa ||
        JSON.stringify([...grants].sort()) !==
          JSON.stringify([...target.direct_permissions].sort())),
  );
  export async function open(account: Account | Group) {
    scoped = null;
    inherit = false;
    const user = "username" in account;
    target = {
      id: account.id,
      label: user ? "@" + account.username : account.name,
      kind: user ? (account.owner_id ? "agent" : "user") : "group",
      direct_permissions: account.direct_permissions,
      direct_require_mfa: account.direct_require_mfa,
      permissions_version: account.permissions_version,
      groups: user ? account.groups : [],
      last_active_superuser: user ? account.last_active_superuser : false,
      member_count: user ? undefined : account.member_count,
    };
    grants = [...account.direct_permissions];
    requireMFA = account.direct_require_mfa;
    catalogue = [];
    category = "Own Account";
    error = "";
    stale = false;
    dialog?.showModal();
    loading = true;
    try {
      catalogue = (
        await api<{ permissions: Permission[] }>(
          target.kind === "agent" ? "agents/permissions" : "permissions",
        )
      ).permissions;
      category = categories[0] ?? "Own Account";
    } catch (e) {
      error = errorMessage(e);
    } finally {
      loading = false;
    }
  }
  export function openScoped(options: ScopedPermissions) {
    scoped = options;
    inherit = options.inherit ?? false;
    grants = [...options.grants];
    requireMFA = false;
    catalogue = options.catalogue;
    category = options.catalogue[0]?.category ?? "Workspace";
    error = "";
    stale = false;
    loading = false;
    target = {
      id: "",
      label: options.label,
      kind: "workspace",
      direct_permissions: [...options.grants],
      direct_require_mfa: false,
      permissions_version: 0,
      groups: [],
      last_active_superuser: false,
    };
    dialog?.showModal();
  }
  function setInheritance(enabled: boolean) {
    if (!enabled && inherit) grants = [...(scoped?.ceiling ?? [])];
    inherit = enabled;
  }
  function sources(permission: string): string[] {
    if (scoped)
      return [
        ...(scoped.superuser ? ["Superuser"] : []),
        ...(scoped.groups ?? [])
          .filter((g) => g.direct_permissions.includes(permission))
          .map((g) => g.name),
      ];
    const out =
      superuser && permission !== "site.superuser" ? ["Superuser"] : [];
    for (const group of target?.groups ?? []) {
      if (group.direct_permissions.includes(permission)) out.push(group.name);
      else if (group.direct_permissions.includes("site.superuser"))
        out.push(`${group.name} (Superuser)`);
    }
    return out;
  }
  const mfaSources = $derived(
    target?.groups.filter((g) => g.direct_require_mfa).map((g) => g.name) ?? [],
  );
  function editable(p: Permission) {
    return (
      !busy &&
      !stale &&
      canGrant(p.id) &&
      !(
        p.id === "site.superuser" &&
        target?.last_active_superuser &&
        sources(p.id).length === 0
      ) &&
      sources(p.id).length === 0
    );
  }
  function change(permission: string, enabled: boolean) {
    grants = enabled
      ? [...grants, permission]
      : grants.filter((p) => p !== permission);
  }
  async function save() {
    if (!target || busy || !dirty || stale) return;
    busy = true;
    error = "";
    try {
      if (scoped) {
        await scoped.save(inherit ? [] : grants, inherit);
        dialog?.close();
        return;
      }
      const value = await api<Account | Group>(
        `${target.kind === "agent" ? "agents" : target.kind === "user" ? "users" : "groups"}/${target.id}/permissions`,
        {
          permissions: grants,
          ...(target.kind === "agent" ? {} : { require_mfa: requireMFA }),
          permissions_version: target.permissions_version,
        },
      );
      dialog?.close();
      if (target.kind !== "group") onSaved(value as Account);
      else onGroupSaved(value as Group);
      window.dispatchEvent(new Event("acta:permissions-changed"));
    } catch (e) {
      error = errorMessage(e);
      stale = e instanceof APIError && e.code === "permissions_changed";
      if (scoped && stale)
        window.dispatchEvent(new Event("acta:workspaces-changed"));
      await tick();
      dialog?.querySelector<HTMLElement>('[role="alert"]')?.focus();
    } finally {
      busy = false;
    }
  }
  function tabKeys(e: KeyboardEvent) {
    const index = categories.indexOf(category);
    const next =
      e.key === "ArrowRight"
        ? (index + 1) % categories.length
        : e.key === "ArrowLeft"
          ? (index + categories.length - 1) % categories.length
          : e.key === "Home"
            ? 0
            : e.key === "End"
              ? categories.length - 1
              : -1;
    if (next >= 0) {
      e.preventDefault();
      category = categories[next];
      void tick().then(() =>
        dialog
          ?.querySelector<HTMLElement>('[role="tab"][aria-selected="true"]')
          ?.focus(),
      );
    }
  }
</script>

<dialog
  bind:this={dialog}
  aria-labelledby={`${id}-title`}
  oncancel={(e) => {
    if (busy) e.preventDefault();
  }}
>
  <header>
    <h2 id={`${id}-title`}>Manage permissions</h2>
    <p>
      For <strong>{target?.label}</strong>{#if target?.kind === "group"}
        · Applies to {target.member_count}
        {target.member_count === 1 ? "member" : "members"}{/if}
    </p>
  </header>
  <div class="tabs" role="tablist" aria-label="Permission categories">
    {#each categories as name, i}<button
        type="button"
        role="tab"
        id={`${id}-tab-${i}`}
        aria-controls={`${id}-panel`}
        aria-selected={category === name}
        tabindex={category === name ? 0 : -1}
        onkeydown={tabKeys}
        onclick={() => (category = name)}>{name}</button
      >{/each}
  </div>
  <div class="body">
    {#if error}<p class="notice error" tabindex="-1" role="alert">
        {error}
      </p>{/if}
    {#if loading}<p class="hint" role="status">Loading permissions…</p>
    {:else if catalogue.length}
      <div
        role="tabpanel"
        id={`${id}-panel`}
        aria-labelledby={`${id}-tab-${categories.indexOf(category)}`}
        tabindex="0"
      >
        {#if scoped?.inherit !== undefined}<div class="permission">
            <div class="copy">
              <label for={`${id}-inherit`}>Inherit from User</label>
              <p>
                Use the owner’s current workspace permissions, including future
                changes.
              </p>
            </div>
            <input
              id={`${id}-inherit`}
              type="checkbox"
              role="switch"
              checked={inherit}
              disabled={busy || stale}
              onchange={(e) => setInheritance(e.currentTarget.checked)}
            />
          </div>{/if}
        {#if !inherit}
          {#each catalogue.filter((p) => p.category === category) as permission (permission.id)}
            {@const direct = grants.includes(permission.id)}
            {@const inherited = sources(permission.id).length > 0}
            <div class="permission">
              <div class="copy">
                <label for={`${id}-${permission.id}`}>{permission.label}</label>
                <p id={`${id}-${permission.id}-description`}>
                  {permission.description}
                </p>
                {#if inherited}<span class="source"
                    >Enabled by {sources(permission.id).join(", ")}{#if direct}
                      · Also granted directly{/if}</span
                  >{/if}
                {#if permission.id === "site.superuser" && target?.last_active_superuser && !inherited}<span
                    class="source"
                    >The last active Superuser must retain this permission.</span
                  >{/if}
              </div>
              <div class="control">
                <input
                  type="checkbox"
                  role="switch"
                  id={`${id}-${permission.id}`}
                  checked={direct || inherited}
                  disabled={!editable(permission)}
                  aria-describedby={`${id}-${permission.id}-description`}
                  onchange={(e) =>
                    change(permission.id, e.currentTarget.checked)}
                />
                {#if inherited && direct && canGrant(permission.id)}<button
                    class="remove"
                    disabled={busy || stale}
                    onclick={() => change(permission.id, false)}
                    >Remove direct grant</button
                  >{/if}
              </div>
            </div>
          {/each}
          {#if category === "Own Account" && target?.kind !== "agent"}<div
              class="permission requirement"
            >
              <div class="copy">
                <label for={`${id}-mfa`}>Require MFA</label>
                <p id={`${id}-mfa-description`}>
                  Require authenticator enrolment before using Acta and prevent
                  disabling MFA. Verified passkeys satisfy sign-in MFA by
                  default. Superuser does not bypass this requirement.
                </p>
                {#if mfaSources.length}<span class="source"
                    >Required by {mfaSources.join(", ")}{#if requireMFA}
                      · Also required directly{/if}</span
                  >{/if}
              </div>
              <div class="control">
                <input
                  type="checkbox"
                  role="switch"
                  id={`${id}-mfa`}
                  checked={requireMFA || mfaSources.length > 0}
                  onchange={(e) => (requireMFA = e.currentTarget.checked)}
                  disabled={busy || stale || mfaSources.length > 0}
                  aria-describedby={`${id}-mfa-description`}
                />
                {#if mfaSources.length && requireMFA}<button
                    class="remove"
                    disabled={busy || stale}
                    onclick={() => (requireMFA = false)}
                    >Remove direct requirement</button
                  >{/if}
              </div>
            </div>{/if}
        {/if}
      </div>
    {:else if !error}<p class="hint">
        You currently have no permissions available to delegate.
      </p>{/if}
  </div>
  <footer>
    <p class="hint">
      {dirty && !scoped && target?.kind !== "agent"
        ? "Existing invitation and recovery links will need replacing."
        : "Changes apply when you save."}
    </p>
    <div>
      <button class="secondary" disabled={busy} onclick={() => dialog?.close()}
        >Cancel</button
      ><button
        class="primary"
        disabled={busy || loading || !dirty || stale || !catalogue.length}
        onclick={save}>{busy ? "Saving…" : "Save changes"}</button
      >
    </div>
  </footer>
</dialog>

<style>
  dialog {
    width: min(680px, calc(100vw - 32px));
    max-height: calc(100svh - 32px);
    padding: 0;
    border: 1px solid var(--panel-border);
    border-radius: 14px;
    background: var(--surface);
    color: var(--text);
    box-shadow: 0 16px 64px #00000030;
  }
  dialog[open] {
    display: flex;
    flex-direction: column;
    animation: appear 140ms ease-out;
  }
  dialog::backdrop {
    background: #00000060;
  }
  header {
    padding: 26px 28px 20px;
  }
  h2 {
    font-size: 21px;
    margin: 0 0 8px;
    letter-spacing: -0.3px;
  }
  header p {
    margin: 0;
    font-size: 13px;
    color: var(--muted);
    overflow-wrap: anywhere;
  }
  strong {
    font-weight: 500;
    color: var(--text);
  }
  .tabs {
    display: flex;
    gap: 20px;
    padding: 0 28px;
    border-bottom: 1px solid var(--panel-border);
    flex-shrink: 0;
  }
  .tabs button {
    background: transparent;
    border: 0;
    border-bottom: 2px solid transparent;
    color: var(--muted);
    padding: 12px 0;
    font-size: 13px;
    white-space: nowrap;
  }
  .tabs button[aria-selected="true"] {
    color: var(--accent);
    border-bottom-color: var(--accent);
  }
  .body {
    padding: 0 28px;
    overflow: auto;
    min-height: 0;
  }
  .permission {
    display: flex;
    align-items: start;
    justify-content: space-between;
    gap: 22px;
    padding: 22px 0;
    border-bottom: 1px solid var(--panel-border);
  }
  .permission:last-child {
    border: 0;
  }
  label {
    font-size: 14px;
    font-weight: 600;
  }
  .copy {
    min-width: 0;
  }
  .copy p {
    font-size: 12px;
    line-height: 1.65;
    color: var(--muted);
    margin: 7px 0 0;
  }
  .source {
    display: block;
    font-size: 11px;
    color: var(--muted);
    margin-top: 9px;
    line-height: 1.5;
  }
  .control {
    display: flex;
    flex-direction: column;
    align-items: flex-end;
    gap: 10px;
    flex-shrink: 0;
  }
  .remove {
    font-size: 11px;
    border: 0;
    color: var(--accent);
    background: transparent;
    max-width: 86px;
    text-align: right;
    padding: 0;
    line-height: 1.4;
  }
  footer {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 20px;
    padding: 20px 28px;
    border-top: 1px solid var(--panel-border);
    flex-shrink: 0;
  }
  footer > div {
    display: flex;
    gap: 10px;
  }
  footer p {
    margin: 0;
  }
  .primary {
    width: auto;
    white-space: nowrap;
  }
  @keyframes appear {
    from {
      opacity: 0;
      transform: translateY(5px);
    }
    to {
      opacity: 1;
      transform: translateY(0);
    }
  }
  @media (max-width: 520px) {
    header {
      padding: 22px 20px 16px;
    }
    .tabs {
      padding: 0 20px;
      gap: 18px;
    }
    .body {
      padding: 0 20px;
    }
    footer {
      padding: 18px 20px;
      flex-wrap: wrap;
    }
    footer > div {
      margin-left: auto;
    }
    .permission {
      gap: 16px;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    dialog[open] {
      animation: none;
      transition: none;
    }
  }
</style>
