<script lang="ts">
  import { useWorkspace } from "$lib/workspaces";
  import ReleaseList from "$lib/components/releases/ReleaseList.svelte";
  const current = useWorkspace();
</script>

{#if current.workspace}{#key current.workspace.id}<ReleaseList
      workspace={current.workspace}
    />{/key}{/if}
