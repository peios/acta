import { api } from "./api";

export type ReleaseState = "planned" | "open" | "frozen" | "released";
export type Release = {
  id: string;
  workspace_id: string;
  name: string;
  codename: string;
  state: ReleaseState;
  description: string;
  total: number;
  finished: number;
  version: number;
  created_at: string;
  updated_at: string;
};

/** Lifecycle order. Any transition is allowed, including backwards. */
export const releaseStates: {
  value: ReleaseState;
  label: string;
  hint: string;
}[] = [
  {
    value: "planned",
    label: "Planned",
    hint: "Work can target it; nobody is working toward it yet.",
  },
  { value: "open", label: "Open", hint: "Taking work." },
  {
    value: "frozen",
    label: "Frozen",
    hint: "No new work starts; work in flight finishes.",
  },
  { value: "released", label: "Released", hint: "Shipped." },
];
export const releaseStateLabel = (state: string) =>
  releaseStates.find((s) => s.value === state)?.label ?? state;

/** Filter and group value for tasks without a target release. */
export const noRelease = "none";

/** The server returns releases in natural name order. */
export async function loadReleases(workspace: string, signal?: AbortSignal) {
  const r = await api<{ releases: Release[] }>(
    `workspaces/${workspace}/releases`,
    undefined,
    { signal },
  );
  return r.releases;
}

/** Picker options: no release first, then every release in name order. */
export function releaseOptions(releases: { id: string; name: string }[]) {
  return [
    { value: "", label: "None" },
    ...releases.map((r) => ({ value: r.id, label: r.name })),
  ];
}
