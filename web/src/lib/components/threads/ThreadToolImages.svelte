<script lang="ts">
  let { images, label }: { images: unknown; label: string } = $props();
  let dialog = $state<HTMLDialogElement>();
  let selected = $state(0);
  let failed = $state<Record<string, boolean>>({});
  const sources = $derived(
    Array.isArray(images)
      ? images.flatMap((image) =>
          image &&
          ["image/png", "image/jpeg", "image/gif", "image/webp"].includes(
            image.media_type,
          ) &&
          typeof image.base64 === "string" &&
          image.base64.length
            ? [`data:${image.media_type};base64,${image.base64}`]
            : [],
        )
      : [],
  );
</script>

<div class="images">
  {#each sources as src, index}
    {#if failed[src]}
      <p class="unavailable">Image {index + 1} could not be displayed.</p>
    {:else}
      <button
        class="thumbnail"
        type="button"
        aria-label={`Enlarge image ${index + 1}: ${label}`}
        onclick={() => {
          selected = index;
          dialog?.showModal();
        }}
      >
        <img
          {src}
          alt={`${label} — image ${index + 1}`}
          loading="lazy"
          onerror={() => (failed[src] = true)}
        />
        <span class="expand"
          ><svg viewBox="0 0 20 20" aria-hidden="true"
            ><path d="M7 3H3v4m10-4h4v4M3 13v4h4m10-4v4h-4" /></svg
          ></span
        >
      </button>
    {/if}
  {/each}
</div>

<dialog bind:this={dialog} aria-label={`${label} — image preview`}>
  <div class="preview-header">
    <span
      >{label}
      <span class="count">{selected + 1} / {sources.length}</span></span
    >
    <button
      class="close"
      type="button"
      aria-label="Close image preview"
      onclick={() => dialog?.close()}
    >
      <svg viewBox="0 0 20 20" aria-hidden="true"
        ><path d="m5 5 10 10M5 15 15 5" /></svg
      >
    </button>
  </div>
  {#if sources[selected]}<img
      class="preview"
      src={sources[selected]}
      alt={`${label} — image ${selected + 1}`}
    />{/if}
</dialog>

<style>
  .images {
    display: flex;
    flex-wrap: wrap;
    gap: 12px;
  }
  .thumbnail {
    position: relative;
    display: grid;
    place-items: center;
    padding: 8px;
    border: 1px solid var(--panel-border);
    border-radius: 10px;
    background: var(--hover-surface);
    cursor: zoom-in;
    overflow: hidden;
    min-width: 100px;
    min-height: 80px;
    max-width: 100%;
    transition: border-color 160ms ease;
  }
  .thumbnail:hover {
    border-color: var(--muted);
  }
  .thumbnail img {
    display: block;
    max-width: min(280px, 100%);
    max-height: 180px;
    object-fit: contain;
  }
  .expand {
    position: absolute;
    right: 5px;
    bottom: 5px;
    display: grid;
    padding: 4px;
    background: var(--surface);
    color: var(--text);
    border-radius: 5px;
  }
  svg {
    width: 16px;
    height: 16px;
    fill: none;
    stroke: currentColor;
    stroke-width: 1.5;
    stroke-linecap: round;
    stroke-linejoin: round;
  }
  dialog {
    color: var(--text);
    background: var(--surface);
    border: 1px solid var(--panel-border);
    border-radius: 14px;
    padding: 0;
    width: max-content;
    max-width: calc(100vw - 32px);
    max-height: calc(100dvh - 32px);
    box-shadow: 0 20px 80px #0006;
  }
  dialog::backdrop {
    background: #000b;
  }
  .preview-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 20px;
    padding: 12px 16px;
    font-size: 13px;
    border-bottom: 1px solid var(--panel-border);
  }
  .count {
    color: var(--muted);
    margin-left: 8px;
    font-size: 11px;
  }
  .close {
    display: grid;
    place-items: center;
    flex-shrink: 0;
    width: 32px;
    height: 32px;
    border: 0;
    border-radius: 7px;
    background: transparent;
    color: var(--muted);
    cursor: pointer;
  }
  .close:hover {
    color: var(--text);
    background: var(--hover-surface);
  }
  .preview {
    display: block;
    margin: 16px auto;
    min-width: min(256px, 70vw);
    max-width: calc(100vw - 64px);
    max-height: calc(100dvh - 140px);
    object-fit: contain;
  }
  .unavailable {
    color: var(--muted);
  }
  button:focus-visible {
    outline: 2px solid var(--text);
    outline-offset: 2px;
  }
</style>
