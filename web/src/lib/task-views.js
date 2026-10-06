/** Filters as saved. Views saved before a category existed omit it (the server may send null); that means no selection.
 * @typedef {{statuses: string[], assignees: string[], unassigned: boolean, priorities?: string[] | null, types?: string[] | null, sizes?: string[] | null, releases?: string[] | null}} SavedViewFilters */
/** Normalised filters, with every category present. copyViewFilters produces them.
 * @typedef {{statuses: string[], assignees: string[], unassigned: boolean, priorities: string[], types: string[], sizes: string[], releases: string[]}} ViewFilters */
/** @returns {ViewFilters} */
export function emptyViewFilters() {
  return {
    priorities: [],
    types: [],
    sizes: [],
    releases: [],
    statuses: [],
    assignees: [],
    unassigned: false,
  };
}
/** @param {ViewFilters} f */
export function hasViewFilters(f) {
  return (
    f.unassigned ||
    [f.priorities, f.types, f.sizes, f.releases, f.statuses, f.assignees].some(
      (values) => values.length > 0,
    )
  );
}
/** Adds a task list query's filter parameters. A status group replaces the status selection.
 * @param {URLSearchParams} q @param {ViewFilters} f @param {string} [groupStatus] */
export function appendViewFilters(q, f, groupStatus = "") {
  for (const id of groupStatus ? [groupStatus] : f.statuses)
    q.append("status", id);
  for (const [key, values] of /** @type {[string, string[]][]} */ ([
    ["priority", f.priorities],
    ["type", f.types],
    ["size", f.sizes],
    ["release", f.releases],
    ["assignee", f.assignees],
  ]))
    for (const value of values) q.append(key, value);
  if (f.unassigned) q.set("unassigned", "true");
}
/** @param {SavedViewFilters} filters @returns {ViewFilters} */
export function copyViewFilters(filters) {
  return {
    priorities: [...(filters.priorities ?? [])],
    types: [...(filters.types ?? [])],
    sizes: [...(filters.sizes ?? [])],
    releases: [...(filters.releases ?? [])],
    statuses: [...filters.statuses],
    assignees: [...filters.assignees],
    unassigned: filters.unassigned,
  };
}
/** @param {SavedViewFilters} a @param {SavedViewFilters} b */
export function sameViewFilters(a, b) {
  /** @param {string[]} values */
  const key = (values) => JSON.stringify([...new Set(values)].sort());
  return (
    key(a.priorities ?? []) === key(b.priorities ?? []) &&
    key(a.types ?? []) === key(b.types ?? []) &&
    key(a.sizes ?? []) === key(b.sizes ?? []) &&
    key(a.releases ?? []) === key(b.releases ?? []) &&
    a.unassigned === b.unassigned &&
    key(a.statuses) === key(b.statuses) &&
    key(a.assignees) === key(b.assignees)
  );
}

/** @typedef {{mode: string, columns: string[], sort: string, direction: string, group: string, density: string}} ViewDisplay */
/** @typedef {{filters: ViewFilters, display: ViewDisplay}} ViewSettings */
/** @typedef {{filters: SavedViewFilters, display: ViewDisplay}} SavedViewSettings */
/** @returns {ViewDisplay} */
export function defaultViewDisplay() {
  return {
    mode: "table",
    columns: ["status", "assignees"],
    sort: "number",
    direction: "desc",
    group: "none",
    density: "comfortable",
  };
}
/** @returns {ViewSettings} */
export function defaultViewSettings() {
  return { filters: emptyViewFilters(), display: defaultViewDisplay() };
}
/** @param {SavedViewSettings} settings @returns {ViewSettings} */
export function copyViewSettings(settings) {
  return {
    filters: copyViewFilters(settings.filters),
    display: { ...settings.display, columns: [...settings.display.columns] },
  };
}
/** @param {SavedViewSettings} a @param {SavedViewSettings} b */
export function sameViewSettings(a, b) {
  return (
    sameViewFilters(a.filters, b.filters) &&
    a.display.mode === b.display.mode &&
    a.display.sort === b.display.sort &&
    a.display.direction === b.display.direction &&
    a.display.group === b.display.group &&
    a.display.density === b.display.density &&
    JSON.stringify([...a.display.columns].sort()) ===
      JSON.stringify([...b.display.columns].sort())
  );
}

/** @param {string} account @param {string} workspace */
const selectionKey = (account, workspace) =>
  `acta.task-view.${account}.${workspace}`;
/** @param {string} account @param {string} workspace @param {{id: string}[]} views */
export function lastSelectedView(account, workspace, views) {
  try {
    const saved = localStorage.getItem(selectionKey(account, workspace));
    if (views.some((view) => view.id === saved)) return saved ?? "";
  } catch {}
  return views[0]?.id ?? "";
}
/** @param {string} account @param {string} workspace @param {string} id */
export function rememberSelectedView(account, workspace, id) {
  if (!id) return;
  try {
    localStorage.setItem(selectionKey(account, workspace), id);
  } catch {}
}
