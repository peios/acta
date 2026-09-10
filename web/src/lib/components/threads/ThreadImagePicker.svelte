<script lang="ts">
  import { onDestroy } from "svelte";
  import {
    saveImage,
    loadImage,
    removeImage,
    validateImageFiles,
    type DraftImage,
  } from "$lib/thread-image-input.js";
  let {
    images,
    disabled = false,
    onchange,
    onbusy,
  }: {
    images: DraftImage[];
    disabled?: boolean;
    onchange: (images: DraftImage[]) => void;
    onbusy: (busy: boolean) => void;
  } = $props();
  let fileInput: HTMLInputElement;
  let error = $state("");
  let busy = $state(false);
  let alive = true;
  let previews = $state<Record<string, string>>({});
  onDestroy(() => {
    alive = false;
  });
  $effect(() => {
    const refs = images;
    error = "";
    let active = true;
    void Promise.all(
      refs.map(async (ref) => {
        const i = await loadImage(ref);
        return [ref.id, `data:${i.media_type};base64,${i.base64}`];
      }),
    )
      .then((entries) => {
        if (active) previews = Object.fromEntries(entries);
      })
      .catch((e) => {
        if (active) error = e.message;
      });
    return () => {
      active = false;
    };
  });
  export function pick() {
    if (!disabled && !busy) fileInput.click();
  }
  export async function add(list: FileList) {
    if (disabled || busy) return;
    const files = Array.from(list);
    if (!files.length) return;
    const saved: DraftImage[] = [];
    try {
      validateImageFiles(files, images);
      error = "";
      busy = true;
      onbusy(true);
      for (const file of files) saved.push(await saveImage(file));
      if (alive) onchange([...images, ...saved]);
      else for (const image of saved) await removeImage(image);
    } catch (e) {
      for (const image of saved) void removeImage(image).catch(() => {});
      if (alive)
        error = e instanceof Error ? e.message : "Could not attach image.";
    } finally {
      if (alive) {
        busy = false;
        onbusy(false);
      }
    }
  }
</script>

<input
  bind:this={fileInput}
  type="file"
  accept="image/png,image/jpeg,image/gif,image/webp"
  multiple
  hidden
  onchange={(event) => {
    if (event.currentTarget.files) void add(event.currentTarget.files);
    event.currentTarget.value = "";
  }}
/>
{#if images.length}<div class="previews" aria-label="Attached images">
    {#each images as image (image.id)}<div class="image" title={image.name}>
        {#if previews[image.id]}<img
            src={previews[image.id]}
            alt={image.name}
          />{:else}<span>{image.name}</span>{/if}
        <button
          type="button"
          class="remove"
          disabled={disabled || busy}
          aria-label={`Remove ${image.name}`}
          onclick={() => onchange(images.filter((i) => i.id !== image.id))}
          >×</button
        >
      </div>{/each}
  </div>{/if}
{#if busy}<small role="status">Adding images…</small>{/if}
{#if error}<small class="error" role="alert">{error}</small>{/if}

<style>
  .previews {
    display: flex;
    gap: 10px;
    overflow-x: auto;
    padding: 3px 2px;
  }
  .image {
    position: relative;
    flex-shrink: 0;
    width: 82px;
    height: 70px;
    border: 1px solid var(--panel-border);
    border-radius: 10px;
    background: var(--surface);
  }
  img {
    width: 100%;
    height: 100%;
    object-fit: cover;
    border-radius: 9px;
  }
  .image span {
    font-size: 11px;
    overflow-wrap: anywhere;
  }
  .remove {
    position: absolute;
    right: -3px;
    top: -3px;
    width: 21px;
    height: 21px;
    min-height: 0;
    padding: 0;
    display: grid;
    place-items: center;
    border: 1px solid var(--panel-border);
    border-radius: 50%;
    background: var(--surface);
    color: var(--text);
    font-size: 17px;
    line-height: 1;
    box-shadow: 0 1px 5px #0003;
  }
  small {
    font-size: 12px;
    color: var(--muted);
  }
  .error {
    color: var(--danger, #e99c92);
  }
</style>
