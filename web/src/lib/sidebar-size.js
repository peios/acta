export const SIDEBAR_DEFAULT = 264;
export const SIDEBAR_MIN = 220;
export const SIDEBAR_MAX = 400;
export const SIDEBAR_RAIL = 68;
const COLLAPSE_THRESHOLD = 160;
const EXPAND_THRESHOLD = 190;

/** @typedef {{ width: number, collapsed: boolean }} SidebarSize */

/** @param {number} width */
export function clampSidebarWidth(width) {
  return Math.round(Math.min(SIDEBAR_MAX, Math.max(SIDEBAR_MIN, width)));
}

/**
 * Keep the raw drag position separate from the displayed width. The expanded
 * panel stops at its usable minimum; continuing inward deliberately collapses.
 * Different thresholds prevent small reversals from flickering between modes.
 * @param {SidebarSize} current
 * @param {number} candidate
 * @param {number} rememberedWidth Width before this gesture, restored on collapse.
 * @returns {SidebarSize}
 */
export function resizeSidebar(current, candidate, rememberedWidth) {
  if (
    current.collapsed
      ? candidate < EXPAND_THRESHOLD
      : candidate < COLLAPSE_THRESHOLD
  ) {
    return { width: rememberedWidth, collapsed: true };
  }
  return { width: clampSidebarWidth(candidate), collapsed: false };
}

/** @param {string | null} raw @returns {SidebarSize} */
export function restoreSidebarSize(raw) {
  const fallback = { width: SIDEBAR_DEFAULT, collapsed: false };
  if (!raw) return fallback;
  try {
    const saved = JSON.parse(raw);
    if (
      !saved ||
      typeof saved.width !== "number" ||
      !Number.isFinite(saved.width) ||
      typeof saved.collapsed !== "boolean"
    )
      return fallback;
    return {
      width: clampSidebarWidth(saved.width),
      collapsed: saved.collapsed,
    };
  } catch {
    return fallback;
  }
}
