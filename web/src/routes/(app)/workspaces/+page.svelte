<script lang="ts">
  import { onMount } from "svelte";
  import { goto } from "$app/navigation";
  import { api, errorMessage } from "$lib/api";
  import { useAccount } from "$lib/account-context";
  import { checkPermission } from "$lib/permissions";
  import { useWorkspace, workspacePath, type Workspace } from "$lib/workspaces";
  import CreateWorkspaceDialog from "$lib/components/workspaces/CreateWorkspaceDialog.svelte";
  import "$lib/components/management/management.css";
  const account = useAccount(),
    current = useWorkspace();
  let loading = $state(true),
    error = $state(""),
    create = $state<CreateWorkspaceDialog>();
  async function load() {
    loading = true;
    error = "";
    current.update(null);
    try {
      const r = await api<{ workspaces: Workspace[] }>("workspaces");
      if (r.workspaces.length) {
        await goto(workspacePath(r.workspaces[0]), { replaceState: true });
        return;
      }
    } catch (e) {
      error = errorMessage(e);
    } finally {
      loading = false;
    }
  }
  onMount(load);
</script>

<div class="management-page">
  {#if error}<p class="notice error" role="alert">{error}</p>
    <button class="secondary" onclick={load}>Try again</button
    >{:else if loading}<p class="hint" role="status">
      Opening your workspace…
    </p>{:else}<div class="empty">
      <h2>A place to begin</h2>
      <p class="management-note">
        You don’t have access to any workspaces yet.
      </p>
      {#if checkPermission(account.account, "site.workspaces.create") && !account.account.owner_id}<button
          class="primary"
          onclick={() => create?.open()}>Create workspace</button
        >{:else}<p class="management-note">
          Ask a workspace administrator to add you.
        </p>{/if}
    </div>{/if}
</div>
<CreateWorkspaceDialog bind:this={create} />

<style>
  .empty {
    padding: 60px 0;
  }
  h2 {
    font-size: 25px;
  }
</style>
