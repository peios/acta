import { api } from "./api";
export type TaskStatus = { id: string; name: string; board?: string };
export type TaskConfig = {
  boards?: {
    slug: string;
    name: string;
    creation_status: string;
    completed_status: string;
  }[];
  board?: string;
  prefix: string;
  previous_prefixes: string[];
  statuses: TaskStatus[];
  creation_status: string;
  completed_status: string;
  version: number;
  revision: number;
};
export type TaskSource = { id: string; reference: string; title: string };
export type TaskPerson = {
  owner_id: string;
  id: string;
  username: string;
  display_name: string | null;
  agent: boolean;
  available: boolean;
  sources: TaskSource[];
};
export type Task = {
  board?: string;
  archived: boolean;
  archived_at: string | null;
  id: string;
  workspace_id: string;
  number: number;
  reference: string;
  title: string;
  priority: string;
  type: string;
  size: string;
  description: string;
  status_id: string;
  parent_id: string;
  assignees: TaskPerson[];
  descendant_assignees: TaskPerson[];
  ancestors: TaskSource[];
  children: number;
  document_count?: number;
  versions: Record<string, number>;
  created_at: string;
  updated_at: string;
};
export type TaskPage = {
  total: number;
  tasks: Task[];
  more: boolean;
  next: number;
  cursor: string;
};
export const taskChanged = () =>
  window.dispatchEvent(new Event("acta:tasks-changed"));
export const personName = (p: TaskPerson) => p.display_name || p.username;

/** All task surfaces use the same field-version check when changing status. */
export function setTaskStatus(task: Task, value: string) {
  return api<Task>(`tasks/${task.id}`, {
    field: "status_id",
    value,
    version: task.versions.status_id,
  });
}

export function createTask(
  workspace: string,
  title: string,
  options: {
    board?: string;
    priority?: string;
    type?: string;
    size?: string;
    parent_id?: string;
    status_id?: string;
    assignees?: string[];
  } = {},
) {
  return api<Task>(`workspaces/${workspace}/tasks`, { title, ...options });
}

/** Select a board's entry/completion rules while retaining all status IDs for task pickers. */
export function boardConfig(config: TaskConfig, board: string): TaskConfig {
  const selected = config.boards?.find((b) => b.slug === board);
  return {
    ...config,
    board,
    creation_status: selected?.creation_status ?? config.creation_status,
    completed_status: selected?.completed_status ?? config.completed_status,
  };
}
export function boardStatuses(config: TaskConfig) {
  return config.statuses.filter(
    (s) => (s.board ?? "tasks") === (config.board ?? "tasks"),
  );
}
export function completedStatus(config: TaskConfig, id: string) {
  return config.boards
    ? config.boards.some((b) => b.completed_status === id)
    : config.completed_status === id;
}
export function entryStatus(config: TaskConfig, id: string) {
  return config.boards
    ? config.boards.some((b) => b.creation_status === id)
    : config.creation_status === id;
}
