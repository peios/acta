/** @typedef {import('./thread-feed.js').FeedItem} FeedItem */
/** @typedef {{id:string,sequence:number,output_index:number,revision:number,visibility:string,deleted:boolean,payload:FeedItem}} StoredItem */
/** @typedef {{run_id?:string,lanes?:Record<string,CurrentState>,agents?:Record<string,import('./threads.svelte').ThreadFrame>,background?:Record<string,import('./threads.svelte').ThreadFrame>,frames?:Record<string,import('./threads.svelte').ThreadFrame>,pending?:Record<string,import('./threads.svelte').ThreadFrame>}} CurrentState */
/** @typedef {{items:StoredItem[],current:CurrentState,revision:number,cursor:number,hasMore:boolean,loadingOlder:boolean,initialLoading:boolean,error:string,controlError:string,pending:null|{id:string,action:'kill'|'resume'|'interrupt'|'rename'},busy:boolean}} ThreadSessionState */

/** Owns one route's reads and lifecycle control. Disposing the session aborts
 * requests and prevents stale responses from publishing into another thread.
 */
export class ThreadSession {
  /** @param {{id:string, request:(path:string,body:unknown,signal:AbortSignal)=>Promise<any>, changed:(state:ThreadSessionState)=>void, refreshThreads:(signal:AbortSignal)=>Promise<unknown>, uuid:()=>string}} options */
  constructor(options) {
    this.options = options;
    /** @type {ThreadSessionState} */
    this.state = {
      items: [],
      current: {},
      revision: 0,
      cursor: 0,
      hasMore: false,
      loadingOlder: false,
      initialLoading: true,
      error: "",
      controlError: "",
      pending: null,
      busy: false,
    };
    this.abort = new AbortController();
    this.after = 0;
    this.before = "";
    this.initialized = false;
    this.debug = false;
    this.lane = "";
    this.views = new Map();
    /** @type {number | undefined} */ this.projectionVersion = undefined;
    this.generation = 0;
    this.records = new Map();
    this.loaded = new Set();
    this.boundary = null;
    this.reading = false;
    this.started = false;
    this.polling = false;
    /** @type {ThreadSessionState | undefined} */ this.published = undefined;
    /** @type {Set<ReturnType<typeof setTimeout>>} */ this.timers = new Set();
  }
  get closed() {
    return this.abort.signal.aborted;
  }
  publish() {
    if (this.closed) return;
    const previous = this.published;
    if (
      previous &&
      Object.keys(this.state).every(
        (key) =>
          this.state[/** @type {keyof ThreadSessionState} */ (key)] ===
          previous[/** @type {keyof ThreadSessionState} */ (key)],
      )
    )
      return;
    this.published = { ...this.state };
    this.options.changed(this.published);
  }
  /** @param {string} suffix @param {unknown} [body] */
  request(suffix, body) {
    return this.options.request(
      `threads/${this.options.id}/${suffix}`,
      body,
      AbortSignal.any([this.abort.signal, AbortSignal.timeout(15000)]),
    );
  }
  start() {
    if (this.closed || this.started) return;
    this.started = true;
    this.publish();
    /** @param {()=>Promise<void>} work */
    const loop = async (work) => {
      await work();
      if (!this.closed) {
        const timer = setTimeout(() => {
          this.timers.delete(timer);
          void loop(work);
        }, 750);
        this.timers.add(timer);
      }
    };
    void loop(() => this.refreshFrames());
    void loop(() => this.pollControl());
  }
  /** @param {boolean} value */
  setDebug(value) {
    if (this.debug === value) return;
    this.debug = value;
    this.resetHistory();
  }
  resetHistory() {
    this.views.clear();
    this.generation++;
    this.initialized = false;
    this.records.clear();
    this.loaded.clear();
    this.boundary = null;
    this.state.loadingOlder = false;
    this.after = 0;
    this.before = "";
    this.state.items = [];
    this.state.initialLoading = true;
    this.state.hasMore = false;
    this.publish();
    void this.refreshFrames();
  }
  /** A rebuilt server projection can change item identities without new provider
   * frames. Discard every cached lane before fetching the new newest page.
   * @param {any} page */
  acceptProjection(page) {
    this.validate(page);
    const version = page.projection_version;
    if (version === undefined) return true;
    if (!Number.isSafeInteger(version) || version < 1)
      throw Error("Received an invalid conversation projection version.");
    const changed =
      this.projectionVersion !== undefined &&
      this.projectionVersion !== version;
    this.projectionVersion = version;
    if (changed) {
      this.state.revision = 0;
      this.resetHistory();
      return false;
    }
    return true;
  }
  /** Preserve each lane's loaded window independently; stale responses cannot
   * publish after switching lanes. @param {string} lane */
  setLane(lane) {
    if (lane === this.lane) return;
    const values = {
      after: this.after,
      before: this.before,
      initialized: this.initialized,
      records: this.records,
      loaded: this.loaded,
      boundary: this.boundary,
    };
    this.views.set(this.lane, {
      values,
      state: {
        items: this.state.items,
        hasMore: this.state.hasMore,
        cursor: this.state.cursor,
        initialLoading: this.state.initialLoading,
      },
    });
    this.lane = lane;
    this.generation++;
    const cached = this.views.get(lane);
    if (cached) {
      Object.assign(this, cached.values);
      Object.assign(this.state, cached.state);
    } else {
      this.after = 0;
      this.before = "";
      this.initialized = false;
      this.records = new Map();
      this.loaded = new Set();
      this.boundary = null;
      Object.assign(this.state, {
        items: [],
        hasMore: false,
        cursor: 0,
        initialLoading: true,
      });
    }
    this.state.loadingOlder = false;
    this.state.error = "";
    this.publish();
    void this.refreshFrames();
  }
  /** Validate the whole response before publishing or advancing any cursor.
   * @param {any} page */
  validate(page) {
    if (
      page.thread_id !== this.options.id ||
      (page.lane_id ?? "") !== this.lane ||
      !Array.isArray(page.items) ||
      !Number.isSafeInteger(page.revision) ||
      !Number.isSafeInteger(page.cursor_revision)
    )
      throw Error("Received an invalid conversation page or different thread.");
    for (const item of page.items)
      if (
        item.payload?.frame?.thread_id !== this.options.id ||
        (item.lane_id ?? "") !== this.lane ||
        !Number.isSafeInteger(item.revision)
      )
        throw Error("Received items for a different thread.");
  }
  /** @param {any} page @param {boolean} changes */
  merge(page, changes) {
    this.validate(page);
    let itemsChanged = false;
    for (const item of page.items) {
      const prior = this.records.get(item.id);
      if (!prior || item.revision > prior.revision) {
        this.records.set(item.id, item);
        itemsChanged = true;
      }
    }
    // A revision describes the whole singleton, across all lanes. Re-reading it
    // must not invalidate every derived UI value while the conversation is idle.
    if (
      page.revision > this.state.revision ||
      (page.revision === this.state.revision && !this.initialized)
    ) {
      this.state.current = page.current;
      this.state.revision = page.revision;
    }
    const boundary = this.boundary;
    for (const item of page.items) {
      if (
        !changes ||
        !boundary ||
        item.sequence > boundary.sequence ||
        (item.sequence === boundary.sequence &&
          item.output_index >= boundary.output_index)
      ) {
        if (!this.loaded.has(item.id)) itemsChanged = true;
        this.loaded.add(item.id);
      }
    }
    if (!itemsChanged) return;
    const items = [...this.records.values()]
      .filter(
        (item) =>
          this.loaded.has(item.id) &&
          !item.deleted &&
          (item.visibility === "normal" ||
            (this.debug && item.visibility === "debug")),
      )
      .sort(
        (a, b) =>
          a.sequence - b.sequence ||
          a.output_index - b.output_index ||
          a.id.localeCompare(b.id),
      );
    if (
      items.length !== this.state.items.length ||
      items.some((item, index) => item !== this.state.items[index])
    )
      this.state.items = items;
  }
  async refreshFrames() {
    if (this.closed || this.reading) return;
    this.reading = true;
    const generation = this.generation;
    const initial = !this.initialized;
    try {
      const page = await this.request(
        `conversation?lane=${encodeURIComponent(this.lane)}&debug=${this.debug}${initial ? "" : `&after=${this.after}`}`,
      );
      if (this.closed || generation !== this.generation) return;
      if (!this.acceptProjection(page)) return;
      this.merge(page, !initial);
      this.after = page.cursor_revision;
      this.state.cursor = this.after;
      if (initial) {
        this.before = page.next;
        this.state.hasMore = page.has_more;
        this.boundary = this.state.items[0];
        this.initialized = true;
        this.state.initialLoading = false;
      }
      this.state.error = "";
      this.publish();
      // Drain changes without waiting between catch-up pages.
      if (!initial && page.has_more) {
        const timer = setTimeout(() => {
          this.timers.delete(timer);
          void this.refreshFrames();
        }, 0);
        this.timers.add(timer);
      }
    } catch (e) {
      if (!this.closed && generation === this.generation) {
        this.state.error = String(e instanceof Error ? e.message : e);
        this.publish();
      }
    } finally {
      this.reading = false;
      if (!this.closed && generation !== this.generation)
        void this.refreshFrames();
    }
  }
  async loadOlder() {
    if (
      this.closed ||
      !this.initialized ||
      !this.state.hasMore ||
      this.state.loadingOlder
    )
      return;
    const generation = this.generation;
    this.state.loadingOlder = true;
    this.publish();
    try {
      const page = await this.request(
        `conversation?lane=${encodeURIComponent(this.lane)}&debug=${this.debug}&before=${encodeURIComponent(this.before)}`,
      );
      if (this.closed || generation !== this.generation) return;
      if (!this.acceptProjection(page)) return;
      this.merge(page, false);
      this.before = page.next;
      this.boundary = this.state.items[0];
      this.state.hasMore = page.has_more;
      this.state.error = "";
    } catch (e) {
      if (!this.closed && generation === this.generation)
        this.state.error = String(e instanceof Error ? e.message : e);
    } finally {
      if (!this.closed && generation === this.generation) {
        this.state.loadingOlder = false;
        this.publish();
      }
    }
  }
  /** @param {'kill'|'resume'|'interrupt'|'rename'} action @param {string} [runId] @param {string} [name] */
  async control(action, runId, name) {
    if (this.closed || this.state.busy || this.state.pending) return;
    this.state.busy = true;
    this.state.controlError = "";
    this.publish();
    const id = this.options.uuid();
    try {
      await this.request("control", {
        id,
        action,
        ...(action === "rename" ? { name } : {}),
        ...(action === "interrupt"
          ? { run_id: runId, ...(this.lane ? { lane_id: this.lane } : {}) }
          : {}),
      });
      if (this.closed) return;
      this.state.pending = { id, action };
      this.publish();
      await this.options.refreshThreads(this.abort.signal);
    } catch (error) {
      if (!this.closed)
        this.state.controlError = String(
          error instanceof Error ? error.message : error,
        );
    } finally {
      this.state.busy = false;
      this.publish();
    }
  }
  async pollControl() {
    const pending = this.state.pending;
    if (this.closed || this.polling || !pending) return;
    this.polling = true;
    try {
      const response = await this.request(`control/${pending.id}`);
      if (this.closed || this.state.pending?.id !== pending.id) return;
      if (response.result) {
        this.state.controlError = response.result.error || "";
        this.state.pending = null;
        this.publish();
        await this.options.refreshThreads(this.abort.signal);
      }
    } catch (error) {
      if (!this.closed) {
        this.state.controlError = String(
          error instanceof Error ? error.message : error,
        );
        this.publish();
      }
    } finally {
      this.polling = false;
    }
  }
  close() {
    this.abort.abort();
    for (const timer of this.timers) clearTimeout(timer);
    this.timers.clear();
  }
}
