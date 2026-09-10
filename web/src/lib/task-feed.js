import { LatestRequest, abortableDelay } from "./requests.js";

/** Owns the workspace's single live revision stream. A focus refresh may overlap
 * a poll, but neither can regress an already accepted revision.
 * @param {{workspace:string, request:(path:string,body:undefined,options:{signal:AbortSignal})=>Promise<import("./tasks").TaskConfig>, onConfig:(config:import("./tasks").TaskConfig,changed:boolean)=>void, onError:(error:unknown)=>void,
 * onAccessLost?:()=>void, wait?:typeof abortableDelay}} options
 */
export function createTaskFeed({
  workspace,
  request,
  onConfig,
  onError,
  onAccessLost = () => {},
  wait = abortableDelay,
}) {
  const lifetime = new AbortController();
  const refreshes = new LatestRequest();
  /** @type {import("./tasks").TaskConfig | null} */
  let config = null;
  /** @param {import("./tasks").TaskConfig} next */
  function accept(next) {
    if (lifetime.signal.aborted) return;
    if (!config || next.revision >= config.revision) {
      const changed = !config || next.revision > config.revision;
      config = next;
      onConfig(next, changed);
    }
    onError(null);
  }
  /** @param {unknown} error */
  function failure(error) {
    if (lifetime.signal.aborted) return;
    onError(error);
    if (
      error &&
      typeof error === "object" &&
      "status" in error &&
      [401, 403, 404].includes(Number(error.status))
    ) {
      stop();
      onAccessLost();
    }
  }
  async function refresh() {
    const read = refreshes.begin();
    if (!read.current()) return;
    try {
      const next = await request(
        `workspaces/${workspace}/task-config`,
        undefined,
        { signal: read.signal },
      );
      if (read.current()) accept(next);
    } catch (error) {
      if (read.current()) failure(error);
    }
  }
  async function watch() {
    await refresh();
    while (!lifetime.signal.aborted) {
      try {
        const next = await request(
          `workspaces/${workspace}/task-changes?after=${config?.revision || 0}`,
          undefined,
          { signal: lifetime.signal },
        );
        accept(next);
      } catch (error) {
        failure(error);
        await wait(3000, lifetime.signal);
      }
    }
  }
  function stop() {
    lifetime.abort();
    refreshes.dispose();
  }
  void watch();
  return { refresh, stop };
}
