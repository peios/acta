/** Refresh the visible window atomically, preserving pages opened with Load more.
 * Cursor pages can overlap if a task moves while requests are in flight.
 * @param {(cursor:string)=>Promise<import('./tasks').TaskPage>} request
 * @param {number} minimum
 */
export async function readTaskWindow(request, minimum = 0) {
  const seenCursors = new Set();
  const tasks = new Map();
  let cursor = "";
  while (true) {
    const page = await request(cursor);
    for (const task of page.tasks) tasks.set(task.id, task);
    if (!page.more || tasks.size >= minimum)
      return { ...page, tasks: [...tasks.values()] };
    if (!page.cursor || seenCursors.has(page.cursor))
      throw new Error("Couldn’t refresh the task list. Please try again.");
    seenCursors.add(page.cursor);
    cursor = page.cursor;
  }
}
