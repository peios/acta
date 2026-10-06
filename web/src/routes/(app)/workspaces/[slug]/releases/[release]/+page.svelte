<script lang="ts">
  import { page } from "$app/state";
  import { useWorkspace } from "$lib/workspaces";
  import ReleaseDetail from "$lib/components/releases/ReleaseDetail.svelte";
  const current = useWorkspace();
</script>

{#if current.workspace}{#key `${current.workspace.id}:${page.params.release}`}<ReleaseDetail
      workspace={current.workspace}
      releaseID={page.params.release ?? ""}
    />{/key}{/if}
