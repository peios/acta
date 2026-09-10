export type ActivityReference = { id: string; label: string };
export type ActivityValue = { text?: string; items?: ActivityReference[] };
export type ActivityEntry = {
  comment?: {
    body: string;
    version: number;
    deleted: boolean;
    edited: boolean;
    thread_id?: string;
    reply_to?: string;
    can_edit: boolean;
    can_delete: boolean;
  };
  reply_count?: number;
  thread_unread?: boolean;
  thread_last_event?: string;
  id: string;
  first_event: string;
  last_event: string;
  actor: {
    id: string;
    username: string;
    display_name: string | null;
    owner_id?: string;
  };
  kind: string;
  field?: string;
  reason?: string;
  before: ActivityValue;
  after: ActivityValue;
  started_at: string;
  updated_at: string;
  count: number;
  unread: boolean;
};
export type ActivityPage = {
  latest_entry?: string;
  latest_event?: string;
  entries: ActivityEntry[];
  more: boolean;
  cursor: string;
  unread: boolean;
};
export type ActivityOrder = "asc" | "desc";
export function activityAction(e: ActivityEntry): string {
  if (e.kind === "document.created")
    return `uploaded ${activityValue(e.after)}`;
  if (e.kind === "document.version")
    return `uploaded a new version of ${activityValue(e.after)}`;
  if (e.kind === "document.deleted")
    return `deleted document ${activityValue(e.before)}`;
  if (e.kind === "task.archived") return "archived the task and its subtasks";
  if (e.kind === "task.restored") return "restored the task and its subtasks";
  if (e.kind === "task.created") return "created the task";
  return (
    (
      {
        title: "edited the title",
        description: "edited the description",
        priority: "changed priority",
        type: "changed type",
        size: "changed size",
        status_id: "changed status",
        parent_id: "changed parent",
        assignees: "changed assignees",
      } as Record<string, string>
    )[e.field ?? ""] ?? "updated the task"
  );
}
export function activityValue(v: ActivityValue): string {
  return v.text || v.items?.map((r) => r.label).join(", ") || "None";
}
