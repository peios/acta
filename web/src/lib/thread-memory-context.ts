import { getContext, setContext } from "svelte";
import type { MemoryReference } from "./thread-memory-references.js";
const key = Symbol("thread-memory-links");
export type ThreadMemories = {
  resolve: (id: string) => Promise<MemoryReference | null>;
  open: (id: string) => void;
};
export const provideThreadMemories = (value: ThreadMemories) =>
  setContext(key, value);
export const useThreadMemories = () =>
  getContext<ThreadMemories | undefined>(key);
