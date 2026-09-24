import { getContext, setContext } from "svelte";

export const initialFiles = [
  {
    name: "codebase.ts",
    path: "src/code/codebase.ts",
    language: "TypeScript",
    content: `import type { CodeHost, Codebase } from './types';

// Open a codebase on the selected host.
export async function openCodebase(
  host: CodeHost,
  directory: string,
): Promise<Codebase> {
  const project = await host.open(directory);

  return {
    id: project.id,
    name: project.name,
    files: project.files,
    conversations: [],
  };
}

export function codebaseLabel(codebase: Codebase) {
  return codebase.name;
}
`,
  },
  {
    name: "types.ts",
    path: "src/code/types.ts",
    language: "TypeScript",
    content: `export interface Codebase {
  id: string;
  name: string;
  files: string[];
  conversations: string[];
}

export interface CodeHost {
  open(directory: string): Promise<Codebase>;
}
`,
  },
  {
    name: "README.md",
    path: "README.md",
    language: "Markdown",
    content: `# Acta Code

A place to write code, with agents alongside you.

## This codebase

- Work on your own machine or a connected host.
- Move between editing, conversation and review.
- Keep the work connected to Acta.

This is sample content for the interface preview.
Edits stay in this page and do not write to disk.
`,
  },
];

const key = Symbol("Code UI preview");
function createCodePreview() {
  const state = $state({
    files: initialFiles.map((file) => ({ ...file })),
    selected: 0,
    panel: "conversation",
    editorView: "editor",
    sidebarTab: "agents",
    thread: 0,
    drafts: ["", ""],
    threads: [
      { name: "Build the Code interface", provider: "Claude Code → Codex" },
      { name: "Review the host boundary", provider: "Codex" },
    ],
    openFile(index: number) {
      state.selected = index;
      state.panel = "editor";
      state.editorView = "editor";
    },
  });
  return state;
}
type CodePreview = ReturnType<typeof createCodePreview>;
export function provideCodePreview() {
  setContext(key, createCodePreview());
}
export function useCodePreview(): CodePreview {
  return getContext(key);
}
