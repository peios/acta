const catalogueLifetime = 15 * 60 * 1000;
/** @param {unknown} value @returns {value is ModelOption[]} */
function validCatalogue(value) {
  return (
    Array.isArray(value) &&
    value.length > 0 &&
    value.length <= 256 &&
    value.every(
      (m) =>
        typeof m?.id === "string" &&
        typeof m.name === "string" &&
        typeof m.description === "string" &&
        typeof m.default_effort === "string" &&
        typeof m.fast_mode === "boolean" &&
        (m.fast_description === undefined ||
          typeof m.fast_description === "string") &&
        Array.isArray(m.efforts) &&
        (m.resolved_model === undefined ||
          typeof m.resolved_model === "string") &&
        m.efforts.every(
          (/** @type {any} */ e) =>
            typeof e?.id === "string" && typeof e.description === "string",
        ),
    )
  );
}
/** @typedef {{model:string, effort:string, fast_mode:boolean}} ModelSettings */
/** @typedef {{id:string,resolved_model?:string,name:string,description:string,efforts:{id:string,description:string}[],default_effort:string,fast_mode:boolean,fast_description?:string}} ModelOption */
/** @typedef {{id:string,action:'configure',run_id:string,settings:ModelSettings,confirmed?:ModelSettings,after:number,state:'saving'|'accepted'|'uncertain'}} PendingSettings */
/** @typedef {{models:ModelOption[], loading:boolean, pending:PendingSettings|null, error:string}} ModelState */

/** @param {ModelSettings} a @param {Record<string,unknown>} b */
export function sameModelSettings(a, b) {
  return (
    a.model === b.model &&
    a.effort === (b.effort ?? "") &&
    a.fast_mode === b.fast_mode
  );
}
/** Preserve compatible choices; changing a model must never send an unsupported effort or speed. @param {ModelOption} model @param {Record<string,unknown>} current @returns {ModelSettings} */
export function settingsForModel(model, current) {
  return {
    model: model.id,
    effort: model.efforts.some((e) => e.id === current.effort)
      ? String(current.effort)
      : model.default_effort,
    fast_mode: model.fast_mode && current.fast_mode === true,
  };
}
export class ThreadModelSettings {
  /** @param {{storage:Storage,key:string,catalogueKey?:string,runId:string,request:(body?:unknown,id?:string)=>Promise<any>,changed:(state:ModelState)=>void,uuid:()=>string}} options */
  constructor(options) {
    this.options = options;
    /** @type {ModelState} */
    this.state = { models: [], loading: false, pending: null, error: "" };
    this.closed = false;
    /** @type {string|null} */ this.catalogRequest = null;
    this.polling = false;
    this.catalogStarted = 0;
    this.catalogueSavedAt = 0;
    if (options.catalogueKey) {
      try {
        const cache = JSON.parse(
          options.storage.getItem(options.catalogueKey) || "null",
        );
        if (
          cache?.runId === options.runId &&
          Number.isFinite(cache.savedAt) &&
          Date.now() - cache.savedAt < catalogueLifetime &&
          validCatalogue(cache.models)
        ) {
          this.state.models = cache.models;
          this.catalogueSavedAt = cache.savedAt;
        }
      } catch {
        /* A disposable catalogue cache must never block the controls. */
      }
    }
    /** @type {{data:Record<string,unknown>,sequence:number}|null} */ this.configuration =
      null;
    try {
      const p = JSON.parse(options.storage.getItem(options.key) || "null");
      if (
        p?.run_id === options.runId &&
        p.action === "configure" &&
        typeof p.id === "string" &&
        typeof p.settings?.model === "string" &&
        typeof p.settings?.effort === "string" &&
        typeof p.settings?.fast_mode === "boolean" &&
        Number.isFinite(p.after)
      ) {
        this.state.pending = { ...p, state: "uncertain" };
      }
    } catch {
      this.state.error = "Could not restore a pending settings change.";
    }
    this.publish();
  }
  publish() {
    if (!this.closed) this.options.changed(structuredClone(this.state));
  }
  persist() {
    try {
      if (this.state.pending)
        this.options.storage.setItem(
          this.options.key,
          JSON.stringify(this.state.pending),
        );
      else this.options.storage.removeItem(this.options.key);
      return true;
    } catch {
      this.state.error = "Could not save the pending change in this browser.";
      this.publish();
      return false;
    }
  }
  async load() {
    if (
      this.closed ||
      this.state.loading ||
      this.state.pending ||
      (this.state.models.length &&
        Date.now() - this.catalogueSavedAt < catalogueLifetime)
    )
      return;
    this.catalogRequest = this.options.uuid();
    this.catalogStarted = Date.now();
    this.state.loading = true;
    this.state.error = "";
    this.publish();
    try {
      await this.options.request({
        id: this.catalogRequest,
        action: "models",
        run_id: this.options.runId,
      });
    } catch {
      if (!this.closed) {
        this.catalogRequest = null;
        this.state.loading = false;
        this.state.error = "Could not load the provider’s models. Try again.";
        this.publish();
      }
    }
    await this.poll();
  }
  /** @param {ModelSettings} settings @param {number} after */
  async change(settings, after) {
    if (this.closed || this.state.pending || this.state.loading) return;
    this.state.pending = {
      id: this.options.uuid(),
      action: "configure",
      run_id: this.options.runId,
      settings,
      after,
      state: "saving",
    };
    this.state.error = "";
    if (!this.persist()) {
      this.state.pending = null;
      this.publish();
      return;
    }
    this.publish();
    await this.submit();
  }
  async submit() {
    const p = this.state.pending;
    if (!p || this.closed || p.state === "accepted") return;
    try {
      await this.options.request({
        id: p.id,
        action: p.action,
        run_id: p.run_id,
        settings: p.settings,
      });
    } catch (error) {
      if (!this.closed && this.state.pending?.id === p.id) {
        if (
          p.state === "saving" &&
          error instanceof Error &&
          "status" in error &&
          [400, 401, 403, 404, 409].includes(Number(error.status))
        ) {
          this.state.pending = null;
          this.state.error = error.message;
          this.persist();
          this.publish();
          return;
        }
        p.state = "uncertain";
        this.state.error =
          "Update not yet confirmed. Check again safely using the same request.";
        this.persist();
        this.publish();
      }
    }
    await this.poll();
  }
  async poll() {
    if (this.closed || this.polling) return;
    this.polling = true;
    try {
      const catalog = this.catalogRequest;
      if (catalog && Date.now() - this.catalogStarted > 75000) {
        this.catalogRequest = null;
        this.state.loading = false;
        this.state.error = "The provider did not return its models. Try again.";
        this.publish();
      }
      if (this.catalogRequest && catalog) {
        const response = await this.options.request(undefined, catalog);
        if (this.closed) return;
        if (this.catalogRequest === catalog && response.result) {
          this.catalogRequest = null;
          this.state.loading = false;
          if (
            response.result.outcome === "accepted" &&
            validCatalogue(response.result.models)
          ) {
            this.state.models = response.result.models;
            this.catalogueSavedAt = Date.now();
            if (this.options.catalogueKey)
              try {
                this.options.storage.setItem(
                  this.options.catalogueKey,
                  JSON.stringify({
                    runId: this.options.runId,
                    savedAt: this.catalogueSavedAt,
                    models: this.state.models,
                  }),
                );
              } catch {
                /* Cache storage is optional. */
              }
          } else
            this.state.error =
              response.result.error || "Could not load the provider’s models.";
          this.publish();
        }
      }
      const p = this.state.pending;
      if (p) {
        const response = await this.options.request(undefined, p.id);
        if (this.closed || this.state.pending?.id !== p.id) return;
        const result = response.result;
        if (result?.outcome === "rejected") {
          this.state.pending = null;
          this.state.error =
            result.error || "The provider rejected these settings.";
          this.persist();
          this.publish();
        } else if (result?.outcome === "accepted") {
          if (
            typeof result.settings?.model === "string" &&
            typeof result.settings.effort === "string" &&
            typeof result.settings.fast_mode === "boolean"
          )
            p.confirmed = result.settings;
          p.state = "accepted";
          this.state.error = "";
          this.persist();
          this.publish();
          if (this.configuration)
            this.observe(this.configuration.data, this.configuration.sequence);
        } else if (result?.outcome === "uncertain" && p.state !== "accepted") {
          p.state = "uncertain";
          this.state.error = result.error || "Settings update is unconfirmed.";
          this.persist();
          this.publish();
        }
      }
    } catch {
      /* A lost read never proves a settings update failed. */
    } finally {
      this.polling = false;
    }
  }
  /** @param {Record<string,unknown>} configuration @param {number} sequence */
  observe(configuration, sequence) {
    this.configuration = { data: configuration, sequence };
    const p = this.state.pending;
    if (
      !p ||
      (sequence <= p.after && p.state !== "accepted") ||
      !sameModelSettings(p.confirmed ?? p.settings, configuration)
    )
      return;
    this.state.pending = null;
    this.state.error = "";
    this.persist();
    this.publish();
  }
  close() {
    this.closed = true;
  }
}
