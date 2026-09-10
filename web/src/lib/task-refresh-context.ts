import { getContext, setContext } from "svelte";

type TaskRefresh = { readonly error: string; readonly recovery: number };
const key = Symbol("task-refresh");

export function provideTaskRefresh(value: TaskRefresh) {
  setContext(key, value);
}

export function useTaskRefresh(): TaskRefresh {
  return getContext<TaskRefresh>(key) ?? { error: "", recovery: 0 };
}
