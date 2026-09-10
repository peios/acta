<script lang="ts">
  import { tick } from "svelte";
  import { goto } from "$app/navigation";
  import WorkspaceForm from "./WorkspaceForm.svelte";
  import { workspacePath } from "$lib/workspaces";
  import "$lib/components/management/management.css";
  let dialog = $state<HTMLDialogElement>(),
    opened = $state(false),
    busy = $state(false);
  const id = $props.id();
  export async function open() {
    opened = true;
    dialog?.showModal();
    await tick();
    dialog?.querySelector<HTMLInputElement>("input")?.focus();
  }
</script>

<dialog
  class="management-dialog"
  bind:this={dialog}
  aria-labelledby={id}
  oncancel={(e) => {
    if (busy) e.preventDefault();
  }}
  onclose={() => (opened = false)}
>
  <h2 {id}>Create workspace</h2>
  <p class="management-note">
    A home for related projects and the people working on them.
  </p>
  {#if opened}<WorkspaceForm
      bind:busy
      cancel={() => dialog?.close()}
      onSaved={(w) => {
        dialog?.close();
        void goto(workspacePath(w));
      }}
    />{/if}
</dialog>
