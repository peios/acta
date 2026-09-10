<script lang="ts">
  let {
    value,
    editing,
    onchange,
  }: {
    value: string;
    editing: boolean;
    onchange: (value: string) => void;
  } = $props();
  function load() {
    return Promise.all([
      import("./MarkdownView.svelte"),
      import("./MarkdownEditor.svelte"),
    ]);
  }
  let content = $state(load());
</script>

{#await content}
  <p class="hint" role="status">Loading description…</p>
{:then [view, editor]}
  {#if editing}<editor.default {value} {onchange} disabled={false} />
  {:else}<view.default {value} />{/if}
{:catch}
  <p class="notice error" role="alert">
    Couldn’t load the description. Please try again.
    <button
      class="secondary"
      onclick={() => {
        content = load();
      }}>Retry</button
    >
  </p>
{/await}
