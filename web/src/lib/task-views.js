/** @typedef {{statuses: string[], assignees: string[], unassigned: boolean, priorities?: string[], types?: string[], sizes?: string[]}} ViewFilters */
/** @param {ViewFilters} filters @returns {ViewFilters} */
export function copyViewFilters(filters) {
  return {
    priorities: [...(filters.priorities ?? [])],
    types: [...(filters.types ?? [])],
    sizes: [...(filters.sizes ?? [])],
    statuses: [...filters.statuses],
    assignees: [...filters.assignees],
    unassigned: filters.unassigned,
  };
}
/** @param {ViewFilters} a @param {ViewFilters} b */
export function sameViewFilters(a, b) {
  /** @param {string[]} values */
  const key = (values) => JSON.stringify([...new Set(values)].sort());
  return (
    key(a.priorities ?? []) === key(b.priorities ?? []) &&
    key(a.types ?? []) === key(b.types ?? []) &&
    key(a.sizes ?? []) === key(b.sizes ?? []) &&
    a.unassigned === b.unassigned &&
    key(a.statuses) === key(b.statuses) &&
    key(a.assignees) === key(b.assignees)
  );
}

/** @typedef {{mode: string, columns: string[], sort: string, direction: string, group: string, density: string}} ViewDisplay */
/** @typedef {{filters: ViewFilters, display: ViewDisplay}} ViewSettings */
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
  return {
    filters: {
      priorities: [],
      types: [],
      sizes: [],
      statuses: [],
      assignees: [],
      unassigned: false,
    },
    display: defaultViewDisplay(),
  };
}
/** @param {ViewSettings} settings @returns {ViewSettings} */
export function copyViewSettings(settings) {
  return {
    filters: copyViewFilters(settings.filters),
    display: { ...settings.display, columns: [...settings.display.columns] },
  };
}
/** @param {ViewSettings} a @param {ViewSettings} b */
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
