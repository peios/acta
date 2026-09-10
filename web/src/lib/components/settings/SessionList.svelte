<script lang="ts">
  const toolNames: Record<string, string> = {
    "identity.read": "Account identity",
    "memories.read": "Read memories",
    "memories.write": "Create, edit and delete memories",
    "tasks.read": "Read tasks and documents",
    "tasks.write": "Create/edit tasks and manage documents",
    "migration.assistant": "Migration Assistant · full content impersonation",
  };
  import { api, errorMessage } from "$lib/api";
  import type { SecurityState } from "$lib/security";
  let {
    sessions,
    busy = false,
    onRevoke,
    allLabel = "Sign out all other sessions",
    accountID = "",
    canMigrate = false,
    onAccessChanged,
  }: {
    sessions: SecurityState["sessions"];
    busy?: boolean;
    onRevoke: (id?: string) => void;
    allLabel?: string;
    accountID?: string;
    canMigrate?: boolean;
    onAccessChanged: () => void | Promise<void>;
  } = $props();
  let editing = $state(""),
    selected = $state<string[]>([]),
    previous = $state<string[]>([]),
    updating = $state(false),
    error = $state(""),
    message = $state("");
  function edit(session: SecurityState["sessions"][number]) {
    editing = session.id;
    previous = [...(session.tool_grants ?? [])];
    selected = [...previous];
    error = "";
    message = "";
  }
  async function save() {
    if (busy || updating) return;
    updating = true;
    error = "";
    try {
      await api(`security/sessions/${editing}/grants`, {
        account_id: accountID,
        previous_grants: previous,
        tool_grants: selected,
      });
      editing = "";
      message =
        "Access updated. Refresh or reconnect your MCP client if its tool list has not changed.";
      await onAccessChanged();
    } catch (e) {
      error = errorMessage(e);
    } finally {
      updating = false;
    }
  }
  const id = $props.id();
  const date = (value: string) =>
    new Intl.DateTimeFormat(undefined, {
      dateStyle: "medium",
      timeStyle: "short",
    }).format(new Date(value));
</script>

<section aria-labelledby={`${id}-heading`}>
  <div class="heading">
    <h2 id={`${id}-heading`}>Sessions</h2>
    {#if sessions.some((s) => !s.current)}<button
        class="text-button"
        disabled={busy || updating}
        onclick={() => onRevoke()}>{allLabel}</button
      >{/if}
  </div>
  <p class="description">
    Browser and device descriptions are approximate. Sessions expire after seven
    days of inactivity, or 30 days in total.
  </p>
  {#if !sessions.length}<p class="description">No active sessions.</p>{/if}
  {#if message}<p class="description" role="status">{message}</p>{/if}
  <ul>
    {#each sessions as session (session.id)}<li>
        <div class="info">
          <strong
            >{session.description}{#if session.current}<span class="current"
                >This session</span
              >{/if}</strong
          >
          <span>Signed in {date(session.created_at)}</span><span
            >Last active {date(session.last_seen_at)}</span
          >
          {#if session.kind === "mcp"}<span
              >Access: {(session.tool_grants ?? [])
                .map((grant) => toolNames[grant] ?? grant)
                .join(", ") || "Guide only"}</span
            >{/if}
          {#if editing === session.id}
            <form
              class="access-editor"
              onsubmit={(event) => {
                event.preventDefault();
                void save();
              }}
            >
              <fieldset disabled={busy || updating}>
                <legend>Connection access</legend>
                <p class="description">
                  Choose the tools this connection can use. Your account and
                  workspace permissions apply to ordinary tools.
                </p>
                {#each Object.entries(toolNames).filter(([grant]) => grant !== "migration.assistant" || canMigrate || previous.includes(grant)) as [grant, label]}
                  <label
                    class:migration-option={grant === "migration.assistant"}
                    ><input
                      type="checkbox"
                      value={grant}
                      disabled={grant === "migration.assistant" &&
                        !canMigrate &&
                        !selected.includes(grant)}
                      bind:group={selected}
                    />{label}</label
                  >
                  {#if grant === "migration.assistant"}
                    <p class="description migration-help">
                      Create and edit all site content as any user or agent,
                      including protected dates and task references.
                      Notifications are suppressed; the actual operator is
                      recorded. Cannot grant permissions or access credentials.
                    </p>
                  {/if}
                {/each}
              </fieldset>
              {#if error}<p class="error" role="alert">{error}</p>{/if}
              <div class="editor-actions">
                <button
                  class="primary"
                  type="submit"
                  disabled={busy || updating}
                  >{updating ? "Saving…" : "Save access"}</button
                >
                <button
                  class="secondary"
                  type="button"
                  disabled={updating}
                  onclick={() => {
                    editing = "";
                    error = "";
                  }}>Cancel</button
                >
              </div>
            </form>
          {/if}
        </div>
        <div class="session-actions">
          {#if session.kind === "mcp" && editing !== session.id}
            <button
              class="text-button"
              disabled={busy || updating}
              aria-label={`Edit access for ${session.description}`}
              onclick={() => edit(session)}>Edit access</button
            >
          {/if}
          {#if !session.current}<button
              class="text-button"
              disabled={busy || updating}
              aria-label={`Sign out ${session.description}`}
              onclick={() => onRevoke(session.id)}>Sign out</button
            >{/if}
        </div>
      </li>{/each}
  </ul>
</section>

<style>
  section {
    padding: 28px 0;
    border-top: 1px solid var(--panel-border);
  }
  .heading {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
    flex-wrap: wrap;
  }
  h2 {
    font-size: 16px;
    margin: 0;
  }
  .description {
    color: var(--muted);
    font-size: 13px;
    line-height: 1.7;
  }
  ul {
    padding: 0;
    margin: 18px 0 0;
    list-style: none;
  }
  li {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 20px;
    padding: 17px 0;
    border-top: 1px solid var(--panel-border);
  }
  li:first-child {
    border: 0;
  }
  .info {
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: 6px;
    min-width: 0;
    overflow-wrap: anywhere;
  }
  strong {
    font-size: 14px;
    font-weight: 550;
  }
  .info > span {
    color: var(--muted);
    font-size: 12px;
  }
  .current {
    font-size: 11px;
    font-weight: 400;
    color: var(--muted);
    margin-left: 10px;
    white-space: nowrap;
  }
  .session-actions {
    display: flex;
    gap: 16px;
    align-self: start;
    flex-shrink: 0;
  }
  .access-editor {
    margin-top: 12px;
    padding: 18px;
    background: var(--surface);
    border: 1px solid var(--panel-border);
    border-radius: 12px;
  }
  fieldset {
    border: 0;
    padding: 0;
    margin: 0;
  }
  legend {
    font-size: 13px;
    font-weight: 550;
  }
  label {
    display: flex;
    align-items: center;
    gap: 10px;
    font-size: 13px;
    padding: 7px 0;
    cursor: pointer;
  }
  .migration-option {
    border-top: 1px solid var(--panel-border);
    margin-top: 12px;
    padding-top: 16px;
  }
  .migration-help {
    margin: 2px 0 8px 26px;
  }
  input[type="checkbox"] {
    width: 16px;
    height: 16px;
    accent-color: var(--accent);
  }
  .editor-actions {
    display: flex;
    gap: 10px;
    margin-top: 16px;
  }
  .editor-actions button {
    width: auto;
    font-size: 13px;
    padding: 8px 14px;
  }
  .error {
    color: var(--danger);
    font-size: 13px;
  }
  @media (max-width: 600px) {
    li {
      flex-wrap: wrap;
      gap: 8px;
    }
    .info {
      flex-basis: 100%;
    }
  }
  .text-button {
    border: 0;
    background: transparent;
    color: var(--accent);
    font-size: 12px;
    padding: 6px 0;
    flex-shrink: 0;
  }
</style>
