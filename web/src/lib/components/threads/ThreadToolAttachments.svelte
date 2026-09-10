<script lang="ts">
  import { pdfAttachments } from "$lib/thread-attachments.js";
  let { attachments }: { attachments: unknown } = $props();
  let files = $state<ReturnType<typeof pdfAttachments>>([]);
  $effect(() => {
    const created = pdfAttachments(attachments);
    files = created;
    return () => created.forEach((file) => URL.revokeObjectURL(file.url));
  });
</script>

<div class="attachments">
  {#each files as file}
    <div class="attachment">
      <span class="pdf" aria-hidden="true">PDF</span>
      <div class="details">
        <span class="name" title={file.name}>{file.name}</span><span
          class="size"
          >{file.size < 1024
            ? `${file.size} bytes`
            : `${(file.size / 1024).toFixed(1)} KB`}</span
        >
      </div>
      <div class="actions">
        <a
          href={file.url}
          target="_blank"
          rel="noopener noreferrer"
          aria-label={`Open ${file.name}`}
          >Open<svg viewBox="0 0 20 20" aria-hidden="true"
            ><path d="M11 3h6v6m0-6-9 9M8 4H4v12h12v-4" /></svg
          ></a
        ><a
          href={file.url}
          download={file.name}
          aria-label={`Download ${file.name}`}
          ><svg viewBox="0 0 20 20" aria-hidden="true"
            ><path d="M10 2v10m-4-4 4 4 4-4M4 13v4h12v-4" /></svg
          ><span class="download-label">Download</span></a
        >
      </div>
    </div>
  {/each}
  {#if !files.length}<p>PDF attachment could not be displayed.</p>{/if}
</div>

<style>
  .attachments {
    display: grid;
    gap: 8px;
    margin-bottom: 10px;
  }
  .attachment {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 12px;
    border: 1px solid var(--panel-border);
    border-radius: 10px;
    background: var(--hover-surface);
    min-width: 0;
  }
  .pdf {
    display: grid;
    place-items: center;
    flex-shrink: 0;
    width: 34px;
    height: 42px;
    border: 1px solid var(--panel-border);
    border-radius: 5px 10px 5px 5px;
    color: var(--text);
    font-size: 9px;
    font-weight: 600;
    letter-spacing: 0.05em;
    background: var(--surface);
  }
  .details {
    display: grid;
    gap: 3px;
    min-width: 0;
    flex: 1;
  }
  .name {
    color: var(--text);
    font-size: 12px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .size {
    color: var(--muted);
    font-size: 11px;
  }
  .actions {
    display: flex;
    align-items: center;
    gap: 6px;
  }
  a {
    display: flex;
    align-items: center;
    gap: 6px;
    border-radius: 6px;
    padding: 7px;
    text-decoration: none;
    color: var(--text);
    font-size: 11px;
  }
  a:hover {
    background: var(--surface);
  }
  a:focus-visible {
    outline: 2px solid var(--text);
    outline-offset: 2px;
  }
  svg {
    width: 14px;
    height: 14px;
    stroke: currentColor;
    stroke-width: 1.5;
    fill: none;
    stroke-linecap: round;
    stroke-linejoin: round;
  }
  p {
    margin: 0;
    color: var(--muted);
  }
  @media (max-width: 600px) {
    .download-label {
      display: none;
    }
    .attachment {
      gap: 8px;
    }
  }
</style>
