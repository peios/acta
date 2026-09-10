import { getContext, setContext } from "svelte";
import type { TaskReference } from "./thread-task-references.js";
const key = Symbol("thread-task-links");
export type ThreadTasks = {
  resolve: (id: string) => Promise<TaskReference | null>;
  open: (id: string) => void;
};
export const provideThreadTasks = (value: ThreadTasks) =>
  setContext(key, value);
export const useThreadTasks = () => getContext<ThreadTasks | undefined>(key);
