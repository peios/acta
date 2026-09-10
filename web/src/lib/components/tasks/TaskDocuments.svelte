<script lang="ts">
  import { onDestroy, tick } from "svelte";
  import { taskChanged } from "$lib/tasks";
  import { api, errorMessage } from "$lib/api";
  import {
    documentLimit,
    documentPreview,
    documentSize,
    documentURL,
    type TaskDocument,
    type DocumentVersion,
    type DocumentPage,
    type DocumentHistory,
  } from "$lib/documents";
  import MarkdownView from "./MarkdownView.svelte";
  import RelativeTime from "../RelativeTime.svelte";
  let {
    task,
    editable,
    revision = 0,
  }: { task: string; editable: boolean; revision?: number } = $props();
  let uploadID = "",
    historyRequest = 0;
  let rows = $state<TaskDocument[]>([]),
    cursor = $state(""),
    error = $state(""),
    loading = $state(true);
  let uploadDialog: HTMLDialogElement,
    previewDialog: HTMLDialogElement,
    deleteDialog: HTMLDialogElement;
  let input: HTMLInputElement;
  let replacing = $state<TaskDocument | null>(null),
    deleting = $state<TaskDocument | null>(null);
  let title = $state(""),
    file = $state<File | null>(null),
    busy = $state(false),
    formError = $state("");
  let selected = $state<TaskDocument | null>(null),
    version = $state<DocumentVersion | null>(null);
  let history = $state<DocumentVersion[]>([]),
    before = $state(0),
    historyBusy = $state(false);
  let previewURL = $state(""),
    previewText = $state(""),
    previewError = $state(""),
    previewLoading = $state(false);
  let request = 0,
    disposed = false,
    blobRequest: AbortController | undefined;
  const kind = $derived(
    version ? documentPreview(version.media_type, version.size) : "download",
  );
  async function load(more = false) {
    const generation = ++request;
    loading = true;
    error = "";
    try {
      const page = await api<DocumentPage>(
        `tasks/${task}/documents${more ? `?cursor=${encodeURIComponent(cursor)}` : ""}`,
      );
      if (disposed || generation !== request) return;
      rows = more
        ? [
            ...rows,
            ...page.documents.filter(
              (d) => !rows.some((old) => old.id === d.id),
            ),
          ]
        : page.documents;
      cursor = page.cursor || "";
    } catch (e) {
      if (!disposed && generation === request) error = errorMessage(e);
    } finally {
      if (!disposed && generation === request) loading = false;
    }
  }
  $effect(() => {
    revision;
    void load();
  });
  onDestroy(() => {
    disposed = true;
    request++;
    clearPreview();
  });
  function clearPreview() {
    blobRequest?.abort();
    blobRequest = undefined;
    if (previewURL) URL.revokeObjectURL(previewURL);
    previewURL = "";
    previewText = "";
    previewLoading = false;
  }
  async function showVersion(v: DocumentVersion) {
    clearPreview();
    version = v;
    previewError = "";
    if (documentPreview(v.media_type, v.size) === "download") return;
    const controller = new AbortController();
    blobRequest = controller;
    previewLoading = true;
    try {
      const response = await fetch(documentURL(v), {
        credentials: "same-origin",
        signal: controller.signal,
      });
      if (!response.ok)
        throw new Error(
          "This version could not be loaded. Check your access and try again.",
        );
      const blob = await response.blob();
      if (controller.signal.aborted || disposed) return;
      if (
        ["text", "markdown"].includes(documentPreview(v.media_type, v.size))
      ) {
        const text = await blob.text();
        if (!controller.signal.aborted && !disposed) previewText = text;
      } else previewURL = URL.createObjectURL(blob);
    } catch (e) {
      if (!controller.signal.aborted) previewError = errorMessage(e);
    } finally {
      if (!controller.signal.aborted) previewLoading = false;
    }
  }
  async function openDocument(d: TaskDocument) {
    selected = d;
    historyRequest++;
    historyBusy = false;
    history = [];
    before = 0;
    previewError = "";
    previewDialog.showModal();
    void showVersion(d);
    await loadHistory();
  }
  async function loadHistory() {
    if (!selected || historyBusy) return;
    const id = selected.id;
    const generation = ++historyRequest;
    historyBusy = true;
    try {
      const page = await api<DocumentHistory>(
        `documents/${id}/versions${before ? `?before=${before}` : ""}`,
      );
      if (generation !== historyRequest || selected?.id !== id || disposed)
        return;
      history = [...history, ...page.versions];
      before = page.before || 0;
    } catch (e) {
      if (generation === historyRequest && !disposed)
        previewError = errorMessage(e);
    } finally {
      if (generation === historyRequest) historyBusy = false;
    }
  }
  async function openUpload(d: TaskDocument | null = null) {
    replacing = d;
    uploadID = d?.id || crypto.randomUUID();
    title = d?.title || "";
    file = null;
    formError = "";
    if (input) input.value = "";
    uploadDialog.showModal();
    await tick();
    input.focus();
  }
  function chooseFile(event: Event) {
    const picked = (event.currentTarget as HTMLInputElement).files?.[0] || null;
    file = picked;
    formError = "";
    if (!title && picked) title = picked.name;
    if (picked && picked.size > documentLimit)
      formError = "Choose a file up to 20 MiB.";
  }
  async function save(event: SubmitEvent) {
    event.preventDefault();
    if (!file || file.size > documentLimit || busy) return;
    busy = true;
    formError = "";
    const data = new FormData();
    data.set("file", file);
    data.set("title", title);
    data.set("revision", String(replacing?.revision || 0));
    data.set("id", uploadID);
    try {
      await api<TaskDocument>(`tasks/${task}/documents`, data);
      uploadDialog.close();
      taskChanged();
      await load();
    } catch (e) {
      formError = errorMessage(e);
    } finally {
      busy = false;
    }
  }
  function confirmDelete(d: TaskDocument) {
    deleting = d;
    formError = "";
    deleteDialog.showModal();
  }
  async function remove() {
    if (!deleting || busy) return;
    busy = true;
    formError = "";
    try {
      await api(`documents/${deleting.id}/delete`, {
        revision: deleting.revision,
      });
      deleteDialog.close();
      taskChanged();
      await load();
    } catch (e) {
      formError = errorMessage(e);
    } finally {
      busy = false;
    }
  }
</script>

<div class="documents">
  <div class="toolbar">
    <span
      >{rows.length
        ? `${rows.length}${cursor ? "+" : ""} ${rows.length === 1 && !cursor ? "document" : "documents"}`
        : "Files that belong to this task"}</span
    >
    <div>
      <button
        class="quiet"
        onclick={() => void load()}
        disabled={loading}
        aria-label="Refresh documents"
        title="Refresh documents">↻</button
      >
      {#if editable}<button class="upload" onclick={() => void openUpload()}
          ><span aria-hidden="true">+</span> Upload document</button
        >{/if}
    </div>
  </div>
  {#if error}<p class="error" role="alert">{error}</p>{/if}
  {#if loading && !rows.length}<p class="empty">Loading documents…</p>
  {:else if !rows.length}<div class="empty-state">
      <svg
        width="32"
        height="32"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        stroke-width="1.2"
        aria-hidden="true"
        ><path
          d="M14 3H6a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V9zM14 3v6h6M8 13h8M8 17h5"
        /></svg
      ><strong>No documents yet</strong>
      <p>
        Add a brief, reference, design or other task output.<br />Each update
        keeps its previous version.
      </p>
    </div>
  {:else}<div class="document-list">
      {#each rows as d (d.id)}<article class="document-row">
          <button
            class="document-main"
            onclick={() => void openDocument(d)}
            aria-label={`Open ${d.title}`}
            ><span class="file-icon" aria-hidden="true"
              ><svg
                width="22"
                height="22"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="1.4"
                ><path
                  d="M14 3H6a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V9zM14 3v6h6M8 13h8M8 17h5"
                /></svg
              ></span
            ><span class="document-info"
              ><strong>{d.title}</strong><span
                >{d.filename} · {documentSize(d.size)} · <RelativeTime
                  value={d.created_at}
                /></span
              ></span
            ><span class="version">v{d.revision}</span></button
          >
          <div class="row-actions">
            <a
              class="quiet"
              href={documentURL(d)}
              download={d.filename}
              title="Download"
              aria-label={`Download ${d.title}`}>↓</a
            >{#if editable && d.can_write}<button
                class="quiet"
                title="Upload new version"
                aria-label={`Upload new version of ${d.title}`}
                onclick={() => void openUpload(d)}>↑</button
              ><button
                class="quiet delete"
                title="Delete document"
                aria-label={`Delete ${d.title}`}
                onclick={() => confirmDelete(d)}>×</button
              >{/if}
          </div>
        </article>{/each}
    </div>{/if}
  {#if cursor}<button
      class="more"
      disabled={loading}
      onclick={() => void load(true)}>Load more documents</button
    >{/if}
</div>

<dialog
  bind:this={uploadDialog}
  class="management-dialog document-upload"
  aria-label={replacing ? "Upload new version" : "Upload document"}
  oncancel={(e) => {
    if (busy) e.preventDefault();
  }}
>
  <form onsubmit={save}>
    <h2>{replacing ? "Upload new version" : "Add a document"}</h2>
    <p class="hint">
      {replacing
        ? `The current v${replacing.revision} will remain in version history.`
        : "Keep task files and their history together."}
    </p>
    <label class="file-picker"
      >Choose a file <span>Up to 20 MiB</span><input
        bind:this={input}
        type="file"
        required
        disabled={busy}
        onchange={chooseFile}
      /></label
    >
    <label
      >Title<input
        bind:value={title}
        required
        maxlength="200"
        disabled={busy}
        placeholder="Document title"
      /></label
    >
    {#if formError}<p class="error" role="alert">{formError}</p>{/if}
    <div class="actions">
      <button
        type="button"
        class="secondary"
        disabled={busy}
        onclick={() => uploadDialog.close()}>Cancel</button
      ><button
        type="submit"
        disabled={busy || !file || file.size > documentLimit}
        >{busy
          ? "Uploading…"
          : replacing
            ? "Save new version"
            : "Upload"}</button
      >
    </div>
  </form>
</dialog>

<dialog
  bind:this={deleteDialog}
  class="management-dialog document-upload"
  aria-label="Delete document"
  oncancel={(e) => {
    if (busy) e.preventDefault();
  }}
>
  <h2>Delete {deleting?.title}?</h2>
  <p class="hint">
    This deletes the document and all its file versions. A record remains in
    task activity.
  </p>
  {#if formError}<p class="error" role="alert">{formError}</p>{/if}
  <div class="actions">
    <button
      class="secondary"
      disabled={busy}
      onclick={() => deleteDialog.close()}>Cancel</button
    ><button class="danger" disabled={busy} onclick={() => void remove()}
      >{busy ? "Deleting…" : "Delete document"}</button
    >
  </div>
</dialog>

<dialog
  bind:this={previewDialog}
  class="management-dialog document-preview"
  aria-label="Document preview"
  onclose={() => {
    clearPreview();
    selected = null;
  }}
>
  {#if selected && version}<header>
      <div>
        <h2>{selected.title}</h2>
        <span
          >{version.filename} · {documentSize(version.size)} · v{version.revision}</span
        >
      </div>
      <a
        class="download-link"
        href={documentURL(version)}
        download={version.filename}>Download</a
      ><button
        class="quiet close"
        aria-label="Close document preview"
        onclick={() => previewDialog.close()}>×</button
      >
    </header>
    <div class="preview-layout">
      <div class="preview-body">
        {#if previewError}<p class="error" role="alert">{previewError}</p>{/if}
        {#if previewLoading}<p class="empty">Loading preview…</p>
        {:else if kind === "image" && previewURL}<img
            src={previewURL}
            alt={version.title}
          />
        {:else if kind === "pdf" && previewURL}<iframe
            title={version.title}
            src={previewURL}
          ></iframe>
        {:else if kind === "markdown"}<div class="markdown">
            <MarkdownView value={previewText} />
          </div>
        {:else if kind === "text"}<pre>{previewText}</pre>
        {:else if !previewError}<div class="empty-state">
            <strong>Ready to download</strong>
            <p>This file type has no preview yet.</p>
            <a href={documentURL(version)} download={version.filename}
              >Download {version.filename}</a
            >
          </div>{/if}
      </div>
      <aside aria-label="Version history">
        <h3>Version history</h3>
        {#each history as v (v.file_id)}<button
            class:current={v.revision === version.revision}
            onclick={() => void showVersion(v)}
            ><strong
              >Version {v.revision}{v.revision === selected.revision
                ? " · Latest"
                : ""}</strong
            ><span
              ><RelativeTime value={v.created_at} /> · {documentSize(
                v.size,
              )}</span
            ></button
          >{/each}{#if before}<button
            disabled={historyBusy}
            onclick={() => void loadHistory()}>Older versions</button
          >{/if}
      </aside>
    </div>{/if}
</dialog>

<style>
  .toolbar,
  .toolbar > div,
  .row-actions,
  .document-row,
  .document-main,
  .actions,
  header {
    display: flex;
    align-items: center;
  }
  .toolbar {
    justify-content: space-between;
    gap: 12px;
    margin: 12px 0 14px;
    color: var(--muted);
    font-size: 12px;
  }
  .toolbar > div {
    gap: 8px;
  }
  button.quiet,
  a.quiet {
    border: 0;
    background: transparent;
    color: var(--muted);
    box-shadow: none;
    min-height: 30px;
    min-width: 28px;
    padding: 4px 7px;
    display: inline-grid;
    place-items: center;
    text-decoration: none;
    font-size: 20px;
    border-radius: 7px;
  }
  .quiet:hover {
    background: var(--hover);
    color: var(--text);
  }
  .quiet.delete:hover {
    color: var(--danger, #ef9188);
  }
  .upload {
    font-size: 12px;
    padding: 6px 10px;
    min-height: 32px;
    background: var(--scope-active);
    color: var(--text);
    border: 1px solid var(--panel-border);
    box-shadow: none;
    border-radius: 8px;
  }
  .upload span {
    font-size: 18px;
    vertical-align: -1px;
    margin-right: 4px;
  }
  .document-list {
    display: grid;
    gap: 8px;
  }
  .document-row {
    border: 1px solid var(--panel-border);
    background: var(--panel);
    border-radius: 12px;
    padding: 4px 7px;
    gap: 4px;
    min-width: 0;
  }
  .document-main {
    flex: 1;
    min-width: 0;
    gap: 11px;
    text-align: left;
    background: transparent;
    color: var(--text);
    border: 0;
    box-shadow: none;
    padding: 11px 5px;
    border-radius: 8px;
  }
  .file-icon {
    color: var(--accent);
    background: var(--scope-active);
    padding: 10px;
    border-radius: 10px;
    display: grid;
  }
  .document-info {
    display: grid;
    gap: 5px;
    min-width: 0;
    flex: 1;
  }
  .document-info strong {
    font-size: 13px;
    font-weight: 550;
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
  }
  .document-info > span {
    font-size: 11px;
    color: var(--muted);
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
  }
  .version {
    font-size: 10px;
    color: var(--muted);
    border: 1px solid var(--panel-border);
    padding: 2px 6px;
    border-radius: 5px;
  }
  .empty-state {
    display: grid;
    justify-items: center;
    text-align: center;
    padding: 35px 15px;
    gap: 12px;
    color: var(--muted);
  }
  .empty-state strong {
    font-size: 14px;
    font-weight: 550;
    color: var(--text);
  }
  .empty-state p {
    font-size: 12px;
    line-height: 1.8;
    margin: 0;
  }
  .empty {
    font-size: 13px;
    color: var(--muted);
    padding: 20px;
  }
  .more {
    font-size: 12px;
    margin-top: 12px;
    background: transparent;
    color: var(--muted);
    border: 0;
    box-shadow: none;
  }
  .document-upload {
    max-width: 480px;
  }
  .document-upload label {
    display: grid;
    gap: 8px;
    margin: 18px 0;
    font-size: 13px;
  }
  .file-picker {
    padding: 18px;
    border: 1px dashed var(--panel-border);
    border-radius: 10px;
  }
  .file-picker span,
  .hint {
    color: var(--muted);
    font-size: 12px;
    line-height: 1.8;
  }
  .file-picker input {
    font-size: 12px;
    max-width: 100%;
  }
  .actions {
    justify-content: flex-end;
    gap: 9px;
    margin-top: 24px;
  }
  .actions button {
    width: auto;
  }
  .error {
    color: var(--danger, #ef9188);
    font-size: 13px;
    line-height: 1.6;
  }
  .document-preview {
    width: min(1080px, calc(100vw - 32px));
    max-width: none;
    max-height: calc(100dvh - 32px);
    padding: 0;
    border-radius: 15px;
  }
  header {
    gap: 16px;
    padding: 20px;
    border-bottom: 1px solid var(--panel-border);
  }
  header > div {
    flex: 1;
    min-width: 0;
  }
  header h2 {
    font-size: 17px;
    margin: 0 0 5px;
    overflow-wrap: anywhere;
  }
  header span {
    font-size: 11px;
    color: var(--muted);
  }
  .download-link {
    font-size: 12px;
    color: var(--accent);
  }
  .preview-layout {
    display: grid;
    grid-template-columns: minmax(0, 1fr) 195px;
    min-height: 360px;
    height: min(680px, 70dvh);
  }
  .preview-body {
    min-width: 0;
    overflow: auto;
    background: var(--bg);
    padding: 20px;
  }
  .preview-body img {
    display: block;
    max-width: 100%;
    max-height: 100%;
    object-fit: contain;
    margin: auto;
  }
  .preview-body iframe {
    width: 100%;
    height: 100%;
    border: 0;
    min-height: 300px;
  }
  .preview-body pre {
    font-size: 12px;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    line-height: 1.8;
    margin: 0;
  }
  .markdown {
    max-width: 760px;
    margin: 0 auto;
  }
  aside {
    padding: 15px 10px;
    border-left: 1px solid var(--panel-border);
    overflow: auto;
  }
  aside h3 {
    font-size: 11px;
    color: var(--muted);
    font-weight: 500;
    margin: 0 8px 12px;
  }
  aside button {
    display: grid;
    gap: 6px;
    width: 100%;
    text-align: left;
    background: transparent;
    border: 0;
    box-shadow: none;
    color: var(--muted);
    padding: 10px 8px;
    border-radius: 8px;
    margin-bottom: 4px;
  }
  aside button.current {
    background: var(--scope-active);
    color: var(--text);
  }
  aside strong {
    font-size: 12px;
    font-weight: 500;
  }
  aside span {
    font-size: 10px;
  }
  @media (max-width: 600px) {
    .toolbar {
      align-items: flex-start;
    }
    .toolbar > span {
      display: none;
    }
    .toolbar {
      justify-content: flex-end;
    }
    .preview-layout {
      grid-template-columns: 1fr;
      grid-template-rows: minmax(230px, 1fr) 145px;
      height: 76dvh;
    }
    aside {
      border-left: 0;
      border-top: 1px solid var(--panel-border);
    }
    aside button {
      display: inline-grid;
      width: auto;
      min-width: 135px;
      margin-right: 5px;
    }
    .document-preview {
      width: calc(100vw - 12px);
      max-height: calc(100dvh - 12px);
    }
    header {
      padding: 13px;
      gap: 12px;
    }
    .version {
      display: none;
    }
    .file-icon {
      padding: 6px;
    }
    .document-row {
      gap: 0;
    }
    .document-info > span {
      max-width: 100%;
    }
  }
</style>
