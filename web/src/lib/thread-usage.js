/** @typedef {import('./threads.svelte').ThreadFrame} ThreadFrame */
/** @typedef {{id: string, label: string, title: string, percent: number | null, details: {label: string, value: string}[], reportedAt: string | null}} UsageGauge */

/** @param {unknown} value */
function number(value) {
  return typeof value === "number" && Number.isFinite(value) && value >= 0
    ? value
    : null;
}
/** @param {unknown} value @returns {Record<string, unknown>} */
function object(value) {
  return value && typeof value === "object" && !Array.isArray(value)
    ? /** @type {Record<string, unknown>} */ (value)
    : {};
}
/** @param {unknown} value */
function label(value) {
  return typeof value === "string" && value.trim() ? value : null;
}
/** @param {unknown} value */
function date(value) {
  return typeof value === "string" && Number.isFinite(Date.parse(value))
    ? value
    : null;
}
/** @param {number | null} value */
function tokens(value) {
  return value === null ? "Not reported" : value.toLocaleString();
}
/** @param {number | null} minutes */
function windowLabel(minutes) {
  if (!minutes) return "Limit";
  if (minutes % 1440 === 0) return `${minutes / 1440}d`;
  if (minutes % 60 === 0) return `${minutes / 60}h`;
  return `${minutes}m`;
}

/** Snapshots replace previous values; never interpret cumulative usage as context fullness.
 * @param {ThreadFrame[]} frames
 * @param {string | undefined} runId
 * @returns {UsageGauge[]}
 */
export function threadUsage(frames, runId) {
  const current = runId ? frames.filter((frame) => frame.run_id === runId) : [];
  const contextFrame = current.findLast(
    (frame) => frame.kind === "usage/context",
  );
  const context = object(contextFrame?.data?.context);
  const used = number(context.used_tokens),
    capacity = number(context.capacity_tokens);
  /** @type {UsageGauge[]} */
  const gauges = [
    {
      id: "context",
      label: "Ctx",
      title:
        context.estimated === true
          ? "Context usage (estimated)"
          : "Context usage",
      percent:
        used !== null && capacity !== null && capacity > 0
          ? (used / capacity) * 100
          : null,
      details: [
        {
          label:
            context.estimated === true ? "Estimated tokens" : "Used tokens",
          value: tokens(used),
        },
        { label: "Context capacity", value: tokens(capacity) },
      ],
      reportedAt: date(contextFrame?.received_at),
    },
  ];
  /** @type {Map<string, ThreadFrame>} */
  const buckets = new Map();
  for (const frame of current) {
    if (frame.kind === "usage/account")
      buckets.set(label(frame.data?.bucket_id) ?? "default", frame);
  }
  for (const [bucketId, frame] of buckets) {
    const data = frame.data ?? {};
    const bucketName =
      label(data.bucket_name) ?? label(data.bucket_id) ?? "Account";
    const windows = Array.isArray(data.windows) ? data.windows : [];
    windows.forEach((value, index) => {
      const window = object(value);
      const minutes = number(window.duration_minutes);
      const short = windowLabel(minutes);
      const name =
        label(window.name) ?? `${short === "Limit" ? "Usage" : short} limit`;
      const reset = date(window.resets_at);
      const details = [{ label: "Account limit", value: bucketName }];
      // This allowance is model-specific; its duration would make it look
      // identical to Claude's general weekly limit.
      const gaugeLabel =
        bucketId === "claude" && name.toLowerCase() === "fable"
          ? "Fable"
          : short;
      if (gaugeLabel !== short) details.push({ label: "Window", value: short });
      if (label(data.plan_name))
        details.push({ label: "Plan", value: String(data.plan_name) });
      details.push({
        label: "Resets",
        value: reset
          ? new Date(reset).toLocaleString(undefined, {
              dateStyle: "medium",
              timeStyle: "short",
            })
          : "Not reported",
      });
      gauges.push({
        id: JSON.stringify([bucketId, index]),
        label: gaugeLabel,
        title: name,
        percent: number(window.used_percent),
        details,
        reportedAt: date(frame.received_at),
      });
    });
  }
  return gauges;
}
