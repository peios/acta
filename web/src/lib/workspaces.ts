import { getContext, setContext } from "svelte";
import type { Permission } from "./permissions";
export type Workspace = {
  id: string;
  name: string;
  slug: string;
  description: string;
  version: number;
  permissions: string[];
  previous_slugs: string[];
};
export type WorkspaceGroup = {
  id: string;
  name: string;
  direct_permissions: string[];
};
export type WorkspaceMember = {
  id: string;
  username: string;
  display_name: string | null;
  status: string;
  permissions: string[];
  direct_permissions: string[];
  groups: WorkspaceGroup[];
  superuser: boolean;
  can_remove: boolean;
};
export type AgentWorkspacePolicy = {
  workspace: Workspace;
  selected: boolean;
  inherit: boolean;
  permissions: string[];
  owner_permissions: string[];
};
export type AgentWorkspaceAccess = {
  all: boolean;
  version: number;
  workspaces: AgentWorkspacePolicy[];
};
export type ScopedPermissions = {
  label: string;
  grants: string[];
  ceiling: string[];
  groups?: WorkspaceGroup[];
  superuser?: boolean;
  catalogue: Permission[];
  inherit?: boolean;
  save: (grants: string[], inherit: boolean) => Promise<void>;
};
const key = Symbol("workspace");
export type WorkspaceContext = {
  readonly workspace: Workspace | null;
  update: (value: Workspace | null) => void;
};
export function provideWorkspace(value: WorkspaceContext) {
  setContext(key, value);
}
export function useWorkspace() {
  return getContext<WorkspaceContext>(key);
}
export const workspacePath = (w: Workspace) =>
  "/workspaces/" + encodeURIComponent(w.slug);
export const canWorkspace = (w: Workspace | null, p: string) =>
  !!w?.permissions.includes(p);
export const workspaceChanged = () =>
  window.dispatchEvent(new Event("acta:workspaces-changed"));
