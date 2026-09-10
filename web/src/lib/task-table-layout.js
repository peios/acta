/** @typedef {'number' | 'title' | 'status' | 'assignees' | 'priority' | 'type' | 'size'} ColumnID */
/** @typedef {{order: ColumnID[], widths: Record<ColumnID, number>}} TableLayout */
/** @type {{id: ColumnID, label: string, size: 'small' | 'medium' | 'big', minimum: number, required?: boolean}[]} */
export const taskColumns = [
  { id: "number", label: "Task", size: "small", minimum: 104, required: true },
  { id: "title", label: "Title", size: "big", minimum: 160, required: true },
  { id: "priority", label: "Priority", size: "small", minimum: 100 },
  { id: "type", label: "Type", size: "small", minimum: 100 },
  { id: "size", label: "Size", size: "small", minimum: 75 },
  { id: "status", label: "Status", size: "medium", minimum: 100 },
  { id: "assignees", label: "Assignees", size: "medium", minimum: 100 },
];
const weights = { small: 1, medium: 1.5, big: 4 };
/** @returns {TableLayout} */
export function defaultTableLayout() {
  const total = taskColumns.reduce(
    (sum, column) => sum + weights[column.size],
    0,
  );
  return {
    order: taskColumns.map((column) => column.id),
    widths: /** @type {Record<ColumnID, number>} */ (
      Object.fromEntries(
        taskColumns.map((column) => [
          column.id,
          (weights[column.size] / total) * 100,
        ]),
      )
    ),
  };
}
/** @param {TableLayout} layout @param {string[]} properties */
export function visibleColumns(layout, properties) {
  return layout.order
    .map((id) => taskColumns.find((column) => column.id === id))
    .filter((column) => column !== undefined)
    .filter((column) => column.required || properties.includes(column.id));
}
/** Distribute available width proportionally, fixing undersized columns at their minimum.
 * Saved widths remain percentages; pixel minimums are only a rendering constraint.
 * @param {TableLayout} layout @param {typeof taskColumns} columns @param {number} available
 * @returns {Record<string, number>}
 */
export function columnPercentages(layout, columns, available) {
  const width = Math.max(
    available,
    columns.reduce((sum, column) => sum + column.minimum, 0),
  );
  let remaining = width;
  let pending = [...columns];
  /** @type {Record<string, number>} */
  const result = {};
  while (pending.length) {
    const total = pending.reduce(
      (sum, column) => sum + layout.widths[column.id],
      0,
    );
    const limited = pending.filter(
      (column) =>
        (remaining * layout.widths[column.id]) / total < column.minimum,
    );
    if (!limited.length) {
      for (const column of pending)
        result[column.id] =
          ((remaining * layout.widths[column.id]) / total / width) * 100;
      break;
    }
    for (const column of limited) {
      result[column.id] = (column.minimum / width) * 100;
      remaining -= column.minimum;
    }
    pending = pending.filter((column) => !limited.includes(column));
  }
  return result;
}
/** Resize a boundary, preserving the combined width of its two neighbours and hidden properties.
 * @param {TableLayout} layout @param {typeof taskColumns} columns @param {Record<string, number>} rendered
 * @param {number} index @param {number} delta @param {number} tableWidth @returns {TableLayout}
 */
export function resizeColumns(
  layout,
  columns,
  rendered,
  index,
  delta,
  tableWidth,
) {
  const left = columns[index],
    right = columns[index + 1];
  if (!left || !right) return layout;
  const pair = rendered[left.id] + rendered[right.id];
  const next = Math.max(
    (left.minimum / tableWidth) * 100,
    Math.min(
      pair - (right.minimum / tableWidth) * 100,
      rendered[left.id] + delta,
    ),
  );
  const mass = columns.reduce(
    (sum, column) => sum + layout.widths[column.id],
    0,
  );
  const widths = { ...layout.widths };
  for (const column of columns)
    widths[column.id] =
      ((column.id === left.id
        ? next
        : column.id === right.id
          ? pair - next
          : rendered[column.id]) *
        mass) /
      100;
  return { order: [...layout.order], widths };
}
/** @param {TableLayout} layout @param {ColumnID[]} visible @param {ColumnID} source @param {number} destination @returns {TableLayout} */
export function reorderColumn(layout, visible, source, destination) {
  const moved = visible.filter((id) => id !== source);
  moved.splice(Math.max(0, Math.min(moved.length, destination)), 0, source);
  let index = 0;
  return {
    order: layout.order.map((id) =>
      visible.includes(id) ? moved[index++] : id,
    ),
    widths: { ...layout.widths },
  };
}
/** Invalid or unavailable browser storage falls back to property defaults.
 * @param {unknown} value @returns {TableLayout}
 */
export function parseTableLayout(value) {
  const fallback = defaultTableLayout();
  if (!value || typeof value !== "object") return fallback;
  const candidate = /** @type {TableLayout} */ (value);
  if (
    !Array.isArray(candidate.order) ||
    !candidate.widths ||
    typeof candidate.widths !== "object"
  )
    return fallback;
  const known = new Set(fallback.order);
  const order = [...new Set(candidate.order.filter((id) => known.has(id)))];
  for (const id of fallback.order) if (!order.includes(id)) order.push(id);
  const widths = { ...fallback.widths };
  for (const id of order) {
    const value = candidate.widths[id];
    if (
      typeof value === "number" &&
      Number.isFinite(value) &&
      value > 0 &&
      value <= 100
    )
      widths[id] = value;
  }
  const total = Object.values(widths).reduce((sum, width) => sum + width, 0);
  for (const id of order) widths[id] = (widths[id] / total) * 100;
  return { order, widths };
}
/** @param {string} workspace @param {string} preset */
const storageKey = (workspace, preset) =>
  `acta.table-layout.${workspace}.${preset}`;
/** @param {string} workspace @param {string} preset */
export function readTableLayout(workspace, preset) {
  try {
    return parseTableLayout(
      JSON.parse(localStorage.getItem(storageKey(workspace, preset)) || "null"),
    );
  } catch {
    return defaultTableLayout();
  }
}
/** @param {string} workspace @param {string} preset @param {TableLayout} layout */
export function saveTableLayout(workspace, preset, layout) {
  try {
    localStorage.setItem(storageKey(workspace, preset), JSON.stringify(layout));
  } catch {}
}
/** @param {string} workspace @param {string} preset */
export function removeTableLayout(workspace, preset) {
  try {
    localStorage.removeItem(storageKey(workspace, preset));
  } catch {}
}
