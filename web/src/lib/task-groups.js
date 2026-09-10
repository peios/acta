/** @typedef {{id:string, name:string, assign_id:string, available:boolean}} TaskGroup */

/** Moving a roll-up removes every assignment owned by its source human;
 * direct agent grouping removes just that account. Other assignments survive.
 * @param {{id:string,owner_id:string}[]} people
 * @param {string} mode
 * @param {string} source
 * @param {string} destination
 */
export function moveAssignments(people, mode, source, destination) {
  const remaining = people.filter(
    (p) =>
      source === "unassigned" ||
      (mode === "assignee" ? (p.owner_id || p.id) !== source : p.id !== source),
  );
  return [
    ...new Set([
      ...remaining.map((p) => p.id),
      ...(destination ? [destination] : []),
    ]),
  ];
}
