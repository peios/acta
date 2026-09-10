const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
/** @param {unknown} value */
export const referenceUUID = (value) =>
  typeof value === "string" && uuid.test(value);

/** Per-page, bounded, coalesced verification. No provider UUID becomes a link
 * without the current user's API access; errors are briefly cached, not permanent.
 * @template {{id:string}} T */
export class ReferenceResolver {
  /** @param {(id:string, signal:AbortSignal)=>Promise<T>} request */
  constructor(request) {
    this.request = request;
  }
  request;
  abort = new AbortController();
  active = 0;
  /** @type {Array<()=>void>} */
  queue = [];
  /** @type {Map<string,{expires:number,promise:Promise<T|null>}>} */
  cache = new Map();
  /** @param {string} id */
  resolve(id) {
    if (!referenceUUID(id) || this.abort.signal.aborted)
      return Promise.resolve(null);
    const cached = this.cache.get(id);
    if (cached && cached.expires > Date.now()) return cached.promise;
    const entry = {
      expires: Infinity,
      promise: /** @type {Promise<T|null>} */ (Promise.resolve(null)),
    };
    entry.promise = new Promise((resolve) => {
      this.queue.push(async () => {
        if (this.abort.signal.aborted) {
          resolve(null);
          return;
        }
        this.active++;
        try {
          const task = await this.request(id, this.abort.signal);
          entry.expires = Date.now() + 60_000;
          resolve(
            !this.abort.signal.aborted &&
              task.id.toLowerCase() === id.toLowerCase()
              ? task
              : null,
          );
        } catch {
          entry.expires = Date.now() + 10_000;
          resolve(null);
        } finally {
          this.active--;
          this.drain();
        }
      });
    });
    this.cache.set(id, entry);
    this.drain();
    return entry.promise;
  }
  drain() {
    while (this.active < 4 && this.queue.length) this.queue.shift()?.();
  }
  close() {
    this.abort.abort();
    this.cache.clear();
    this.drain();
  }
}
