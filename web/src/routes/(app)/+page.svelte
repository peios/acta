<script lang="ts">
  import { onMount } from "svelte";
  import { goto } from "$app/navigation";
  onMount(() => {
    void goto("/workspaces", { replaceState: true });
  });
</script>

<p class="hint" role="status">Opening Acta…</p>
