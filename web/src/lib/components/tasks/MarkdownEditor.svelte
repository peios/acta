<script lang="ts">
  import { onMount, tick, untrack } from "svelte";
  import { Editor } from "@tiptap/core";
  import "$lib/task-rich-text.css";
  import { taskExtensions, needsSourceMode } from "$lib/task-markdown.js";
  let {
    value,
    onchange,
    disabled = false,
    label = "Task description",
    onsubmit,
    autofocus = true,
  }: {
    value: string;
    onchange: (value: string) => void;
    disabled?: boolean;
    label?: string;
    onsubmit?: () => void;
    autofocus?: boolean;
  } = $props();
  let host: HTMLDivElement;
  let sourceInput = $state<HTMLTextAreaElement>();
  let editor: Editor | undefined;
  let mode = $state("rich");
  const sourceOnly = $derived(needsSourceMode(value));
  onMount(() => {
    try {
      mode =
        localStorage.getItem("acta.task-editor") === "markdown"
          ? "markdown"
          : "rich";
    } catch {}
    if (sourceOnly) mode = "markdown";
    editor = new Editor({
      element: host,
      extensions: taskExtensions(),
      content: value,
      contentType: "markdown",
      editable: !disabled,
      editorProps: {
        handleKeyDown: (_view, event) => {
          if (
            onsubmit &&
            event.key === "Enter" &&
            (event.metaKey || event.ctrlKey)
          ) {
            event.preventDefault();
            onsubmit();
            return true;
          }
          return false;
        },
        attributes: {
          class: "task-rich-text",
          role: "textbox",
          "aria-label": label,
          "aria-multiline": "true",
        },
      },
      onUpdate: ({ editor, transaction }) => {
        // Focus can append an empty trailing paragraph; it is not a user edit.
        if (transaction.docChanged) onchange(editor.getMarkdown());
      },
    });
    if (autofocus) void focusEditor();
    return () => editor?.destroy();
  });
  $effect(() => {
    const v = value,
      d = disabled,
      source = sourceOnly;
    untrack(() => {
      if (source) mode = "markdown";
      if (editor) {
        editor.setEditable(!d && !source, false);
        if (editor.getMarkdown() !== v) {
          const focused = editor.isFocused;
          const selection = editor.state.selection;
          editor.commands.setContent(v, {
            contentType: "markdown",
            emitUpdate: false,
          });
          if (focused) {
            const end = editor.state.doc.content.size;
            editor.commands.setTextSelection({
              from: Math.min(selection.from, end),
              to: Math.min(selection.to, end),
            });
          }
        }
      }
    });
  });
  async function focusEditor() {
    await tick();
    if (!editor || editor.isDestroyed) return;
    if (mode === "rich") editor.commands.focus("end");
    else sourceInput?.focus();
  }
  function choose(next: string) {
    if (next === "rich" && sourceOnly) return;
    mode = next;
    try {
      localStorage.setItem("acta.task-editor", next);
    } catch {}
    if (next === "rich")
      editor?.commands.setContent(value, {
        contentType: "markdown",
        emitUpdate: false,
      });
    void focusEditor();
  }
  function format(action: string) {
    if (!editor) return;
    const c = editor.chain().focus();
    if (action === "bold") c.toggleBold().run();
    if (action === "italic") c.toggleItalic().run();
    if (action === "heading") c.toggleHeading({ level: 2 }).run();
    if (action === "list") c.toggleBulletList().run();
    if (action === "tasks") c.toggleTaskList().run();
    if (action === "quote") c.toggleBlockquote().run();
    if (action === "code") c.toggleCodeBlock().run();
    if (action === "table")
      c.insertTable({ rows: 3, cols: 3, withHeaderRow: true }).run();
  }
</script>

<div class="markdown-editor">
  {#if sourceOnly}<p class="source-notice">
      This content contains images or HTML. Markdown mode preserves that
      content.
    </p>{/if}
  <div class="editor-bar">
    <div class="modes" aria-label="Editor mode">
      <button
        type="button"
        class:chosen={mode === "rich"}
        aria-pressed={mode === "rich"}
        disabled={sourceOnly}
        onclick={() => choose("rich")}>Rich text</button
      ><button
        type="button"
        class:chosen={mode === "markdown"}
        aria-pressed={mode === "markdown"}
        onclick={() => choose("markdown")}>Markdown</button
      >
    </div>
  </div>
  {#if mode === "rich" && !disabled}<div class="tools" aria-label="Formatting">
      {#each [["bold", "Bold"], ["italic", "Italic"], ["heading", "Heading"], ["list", "List"], ["tasks", "Checklist"], ["quote", "Quote"], ["code", "Code"], ["table", "Table"]] as [action, label]}<button
          type="button"
          onclick={() => format(action)}>{label}</button
        >{/each}
    </div>{/if}
  <div class:hidden={mode !== "rich"} class="rich-host" bind:this={host}></div>
  {#if mode === "markdown"}<textarea
      bind:this={sourceInput}
      aria-label={`${label} Markdown`}
      {disabled}
      {value}
      oninput={(e) => onchange(e.currentTarget.value)}
      onkeydown={(event) => {
        if (
          onsubmit &&
          event.key === "Enter" &&
          (event.metaKey || event.ctrlKey)
        ) {
          event.preventDefault();
          onsubmit();
        }
      }}
      spellcheck="false"></textarea>{/if}
</div>

<style>
  .source-notice {
    font-size: 12px;
    color: var(--muted);
    padding: 8px 16px;
  }
  .markdown-editor {
    overflow: hidden;
    background: transparent;
  }
  .editor-bar,
  .tools {
    display: flex;
    gap: 4px;
    padding: 4px 0;
    flex-wrap: wrap;
  }
  button {
    border: 0;
    background: transparent;
    color: var(--muted);
    border-radius: 5px;
    padding: 5px 7px;
    font-size: 12px;
  }
  button:hover,
  .chosen {
    background: var(--hover-surface);
    color: var(--text);
  }
  .hidden {
    display: none;
  }
  .rich-host :global(.tiptap) {
    padding: 10px 0 0;
    min-height: 120px;
    outline: none;
    line-height: 1.7;
    overflow-wrap: anywhere;
  }
  .rich-host :global(.tiptap:focus) {
    box-shadow: inset 0 -1px 0 var(--panel-border);
  }
  textarea {
    border: 0 !important;
    border-radius: 0 !important;
    resize: vertical;
    width: 100%;
    min-height: 160px;
    padding: 10px 0;
    font: 14px/1.7 monospace;
    background: transparent;
    color: var(--text);
  }
</style>
