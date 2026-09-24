<script lang="ts">
  import NavigationToggle from "$lib/components/chrome/NavigationToggle.svelte";
  import ThreadOutput from "$lib/components/code/ThreadOutput.svelte";
  import { initialFiles, useCodePreview } from "$lib/code-preview.svelte";
  import { useCodebases } from "$lib/codebases.svelte";
  const code = useCodePreview(),
    codebases = useCodebases();
  const selectedCodebase = $derived(
    codebases.state.items.find((item) => item.id === codebases.state.selected),
  );
  const file = $derived(code.files[code.selected]);
  const modified = $derived(
    file.content !== initialFiles[code.selected].content,
  );
  let showTerminal = $state(false);
  function openFile(index: number) {
    code.openFile(index);
  }
  function showExampleDiff() {
    openFile(0);
    code.editorView = "changes";
  }
</script>

<svelte:head><title>Code · Acta</title></svelte:head>
<section class="code-codebase" aria-label="Acta Code">
  <header class="codebase-header">
    <NavigationToggle />
    <h1>{selectedCodebase?.name ?? "Code"}</h1>
    {#if selectedCodebase}<span class="muted"
        >{selectedCodebase.roots.length}
        {selectedCodebase.roots.length === 1 ? "folder" : "folders"}</span
      >{/if}
  </header>
  <div class="codebase-toolbar">
    <span class="breadcrumb">{file.path} · editor preview</span>
    <div class="view-switch" aria-label="Codebase view">
      <button
        class:chosen={code.panel === "conversation"}
        aria-pressed={code.panel === "conversation"}
        onclick={() => (code.panel = "conversation")}>Conversation</button
      >
      <button
        class:chosen={code.panel === "editor"}
        aria-pressed={code.panel === "editor"}
        onclick={() => (code.panel = "editor")}>Editor</button
      >
    </div>
    <button
      class="terminal-toggle"
      aria-pressed={showTerminal}
      onclick={() => {
        showTerminal = !showTerminal;
        code.panel = "editor";
      }}>Terminal</button
    >
  </div>
  <div class="panes" class:show-editor={code.panel === "editor"}>
    <div class="conversation-pane"><ThreadOutput /></div>
    <section class="editor-pane" aria-label="Editor preview">
      <div class="file-tabs" aria-label="Open sample files">
        {#each code.files.slice(0, 2) as item, index}<button
            class:active={code.selected === index}
            aria-pressed={code.selected === index}
            onclick={() => openFile(index)}
            ><span class="file-icon">TS</span>{item.name}<span class="tab-mark"
              >{item.content !== initialFiles[index].content ? "●" : ""}</span
            ></button
          >{/each}
        {#if code.selected === 2}<button
            class="active"
            aria-pressed="true"
            onclick={() => openFile(2)}>README.md</button
          >{/if}
      </div>
      <div class="editor-toolbar">
        <span>{file.path}</span>
        <div>
          <button
            class:chosen={code.editorView === "editor"}
            onclick={() => (code.editorView = "editor")}>Source</button
          ><button
            class:chosen={code.editorView === "changes"}
            onclick={showExampleDiff}
            >Example diff <span class="change-count">+3 −1</span></button
          >
        </div>
      </div>
      {#if code.editorView === "editor"}
        <div class="source-editor">
          <div class="line-numbers" aria-hidden="true">
            {#each file.content.split("\n") as _, line}<div>
                {line + 1}
              </div>{/each}
          </div>
          <textarea
            class="source"
            aria-label={`Edit sample ${file.name}`}
            spellcheck="false"
            wrap="off"
            value={file.content}
            oninput={(event) =>
              (code.files[code.selected].content = event.currentTarget.value)}
          ></textarea>
        </div>
      {:else}
        <div class="diff-view">
          <p class="muted">Example change · codebase.ts</p>
          <pre>  return &#123;
    id: project.id,
<span class="removed">−   name: directory,</span>
<span class="added">+   name: project.name,</span>
<span class="added">+   files: project.files,</span>
<span class="added">+   conversations: [],</span>
  &#125;;</pre>
        </div>
      {/if}
      {#if showTerminal}<section class="terminal" aria-label="Sample terminal">
          <div class="terminal-heading">
            <span>Terminal <span class="muted">/ sample output</span></span
            ><button
              aria-label="Close terminal"
              onclick={() => (showTerminal = false)}>×</button
            >
          </div>
          <pre><span class="muted">~/projects/acta</span> $ npm run check
<span class="change-count">✓ No errors found.</span>

<span class="muted">Preview only · no commands are running.</span></pre>
        </section>{/if}
      <footer class="editor-status">
        <span>{modified ? "Edited in preview" : "Sample file"}</span><span
          >{file.language} · UTF-8 · 2 spaces</span
        >
      </footer>
    </section>
  </div>
</section>

<style>
  .code-codebase {
    height: 100%;
    min-height: 0;
    display: flex;
    flex-direction: column;
    container-type: inline-size;
    background: var(--surface);
  }
  button {
    border: 0;
    background: transparent;
    color: var(--muted);
    border-radius: 5px;
    font-size: 12px;
  }
  button:hover {
    background: var(--hover-surface);
    color: var(--text);
  }
  button.chosen {
    color: var(--text);
    background: var(--scope-active);
  }
  .muted {
    color: var(--muted);
  }
  .codebase-header {
    min-height: 64px;
    padding: 0 22px;
    display: flex;
    align-items: center;
    gap: 18px;
    border-bottom: 1px solid var(--panel-border);
    font-size: 12px;
  }
  h1 {
    font-size: 15px;
    margin: 0;
    letter-spacing: 0;
  }
  .codebase-toolbar {
    min-height: 43px;
    padding: 0 16px;
    display: flex;
    align-items: center;
    gap: 14px;
    border-bottom: 1px solid var(--panel-border);
  }
  .codebase-toolbar button {
    min-height: 30px;
    padding: 5px 9px;
  }
  .breadcrumb {
    font-size: 11px;
    color: var(--muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .terminal-toggle {
    margin-left: auto;
  }
  .view-switch {
    display: none;
    margin-left: auto;
    gap: 2px;
  }
  .panes {
    flex: 1;
    min-height: 0;
    display: grid;
    grid-template-columns: minmax(350px, 1fr) minmax(0, 1.2fr);
  }
  .conversation-pane,
  .editor-pane {
    min-width: 0;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }
  .editor-pane {
    border-left: 1px solid var(--panel-border);
  }
  .file-tabs {
    min-height: 43px;
    display: flex;
    flex-shrink: 0;
    overflow: auto;
    border-bottom: 1px solid var(--panel-border);
    background: var(--sidebar-surface);
  }
  .file-tabs button {
    flex-shrink: 0;
    border-radius: 0;
    padding: 0 14px;
    display: flex;
    gap: 9px;
    align-items: center;
    border-right: 1px solid var(--panel-border);
    border-top: 2px solid transparent;
  }
  .file-tabs button.active {
    color: var(--text);
    background: var(--surface);
    border-top-color: var(--accent);
  }
  .file-icon {
    color: var(--accent);
    font:
      600 10px ui-monospace,
      monospace;
  }
  .tab-mark {
    width: 8px;
    font-size: 8px;
  }
  .editor-toolbar {
    min-height: 40px;
    padding: 0 15px;
    display: flex;
    gap: 8px;
    align-items: center;
    justify-content: space-between;
    font-size: 10px;
    color: var(--muted);
    flex-wrap: wrap;
  }
  .editor-toolbar button {
    font-size: 10px;
    padding: 5px 6px;
  }
  .source-editor {
    flex: 1;
    min-height: 180px;
    display: flex;
    overflow: auto;
    padding: 12px 0 26px;
  }
  .line-numbers {
    flex: 0 0 48px;
    padding-right: 15px;
    text-align: right;
    color: var(--muted);
    user-select: none;
    font:
      12px/24px ui-monospace,
      monospace;
  }
  .source {
    flex: 1;
    min-width: 0;
    height: 100%;
    min-height: 530px;
    resize: none;
    padding: 0 18px 0 0;
    margin: 0;
    border: 0;
    border-radius: 0;
    outline-offset: -2px;
    background: transparent;
    color: var(--subtle-text);
    font:
      12px/24px ui-monospace,
      monospace;
    tab-size: 2;
  }
  .editor-status {
    min-height: 29px;
    padding: 0 12px;
    border-top: 1px solid var(--panel-border);
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    color: var(--muted);
    font-size: 10px;
  }
  .diff-view {
    flex: 1;
    padding: 16px;
    overflow: auto;
    font-size: 11px;
  }
  pre {
    font:
      12px/24px ui-monospace,
      monospace;
  }
  .diff-view pre span {
    display: block;
  }
  .removed {
    background: var(--error-surface);
    color: var(--error-text);
  }
  .added {
    background: var(--success-surface);
    color: var(--success-text);
  }
  .terminal {
    border-top: 1px solid var(--panel-border);
    padding: 0 16px 12px;
    max-height: 190px;
    overflow: auto;
  }
  .terminal-heading {
    display: flex;
    align-items: center;
    justify-content: space-between;
    min-height: 38px;
    font-size: 11px;
  }
  .terminal-heading button {
    width: 30px;
    height: 30px;
    font-size: 20px;
  }
  .terminal pre {
    font-size: 11px;
    margin: 0;
  }
  .change-count {
    color: var(--success-text);
  }
  @container (max-width:850px) {
    .panes {
      grid-template-columns: minmax(0, 1fr);
    }
    .editor-pane {
      display: none;
      border-left: 0;
    }
    .panes.show-editor .editor-pane {
      display: flex;
    }
    .panes.show-editor .conversation-pane {
      display: none;
    }
    .view-switch {
      display: flex;
    }
    .terminal-toggle {
      margin-left: 0;
    }
    .breadcrumb {
      display: none;
    }
  }
  @container (max-width:500px) {
    .codebase-header {
      padding-inline: 16px;
      gap: 10px;
    }
    .codebase-toolbar {
      padding-inline: 8px;
      gap: 4px;
    }
    .source {
      font-size: 11px;
    }
    .editor-toolbar > span {
      display: none;
    }
  }
</style>
