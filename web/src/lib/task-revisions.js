// Concurrent independent saves may arrive out of order. Never regress a field.
export function mergeTask(current, incoming) {
  if (!current || current.id !== incoming.id) return incoming;
  const result = { ...incoming, versions: { ...incoming.versions } };
  for (const field of Object.keys(current.versions)) {
    if (current.versions[field] > incoming.versions[field]) {
      result[field] = current[field];
      if (field === "status_id") result.board = current.board;
      if (field === "archived") result.archived_at = current.archived_at;
      result.versions[field] = current.versions[field];
    }
  }
  return result;
}
