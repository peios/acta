<script lang="ts">
  import { goto } from "$app/navigation";
  import { useWorkspace, workspacePath, canWorkspace } from "$lib/workspaces";
  import TaskConfiguration from "$lib/components/tasks/TaskConfiguration.svelte";
  import WorkspaceForm from "$lib/components/workspaces/WorkspaceForm.svelte";
  import "$lib/components/management/management.css";
  const current = useWorkspace();
</script>

{#if current.workspace && (canWorkspace(current.workspace, "workspace.edit") || canWorkspace(current.workspace, "tasks.statuses.manage"))}<div
    class="management-page"
  >
    <p class="management-note">
      Manage this workspace’s name, address and description.
    </p>
    {#if canWorkspace(current.workspace, "workspace.edit")}
      {#key current.workspace.id}<WorkspaceForm
          workspace={current.workspace}
          onSaved={(w) => {
            current.update(w);
            void goto(workspacePath(w) + "/settings/details", {
              replaceState: true,
            });
          }}
        />{/key}
    {:else}<h2>{current.workspace.name}</h2>
      <p>{current.workspace.description}</p>{/if}
    {#key current.workspace.id}<TaskConfiguration
        workspace={current.workspace}
      />{/key}
  </div>{:else}<p class="notice error" role="alert">
    You don’t have permission to edit this workspace.
  </p>{/if}
