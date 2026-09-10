<script lang="ts">
  import { untrack } from "svelte";
  import { page } from "$app/state";
  import { goto } from "$app/navigation";
  import { api, errorMessage } from "$lib/api";
  import { workspacePath, type Workspace } from "$lib/workspaces";
  import type { Task } from "$lib/tasks";
  let error = $state("");
  const taskID = $derived(page.params.id);
  let retry = $state(0);
  $effect(() => {
    const id = taskID;
    void retry;
    const controller = new AbortController();
    error = "";
    untrack(
      () =>
        void (async () => {
          try {
            const t = await api<Task>(
              "tasks/" + encodeURIComponent(id!),
              undefined,
              { signal: controller.signal },
            );
            const w = await api<Workspace>(
              "workspaces/" + t.workspace_id,
              undefined,
              { signal: controller.signal },
            );
            if (!controller.signal.aborted)
              await goto(workspacePath(w) + "?task=" + t.id, {
                replaceState: true,
              });
          } catch (e) {
            if (!controller.signal.aborted) error = errorMessage(e);
          }
        })(),
    );
    return () => controller.abort();
  });
</script>

{#if error}<p class="notice error" role="alert">{error}</p>
  <button class="secondary" onclick={() => retry++}>Try again</button>
{:else}<p class="hint" role="status">Opening task…</p>{/if}
