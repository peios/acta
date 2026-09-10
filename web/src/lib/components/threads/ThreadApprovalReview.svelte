<script lang="ts">
  import type { ApprovalReviewItem } from "$lib/thread-approval-reviews.js";
  let {
    review,
    live,
    showTool,
  }: { review: ApprovalReviewItem; live: boolean; showTool?: () => void } =
    $props();
  const data = $derived(review.data);
  const details = $derived(data.details ?? {});
  const busy = $derived(
    data.status === "in_progress" && live && !review.interrupted,
  );
  const label = $derived(
    data.status === "in_progress"
      ? busy
        ? "Reviewing permission…"
        : "Review unavailable"
      : ((
          {
            approved: "Automatically approved",
            denied: "Automatically denied",
            timed_out: "Review timed out",
            aborted: "Review cancelled",
          } as Record<string, string>
        )[data.status] ?? "Review ended"),
  );
  const duration = $derived(
    typeof data.started_at === "string" && typeof data.completed_at === "string"
      ? Math.max(
          0,
          Date.parse(data.completed_at) - Date.parse(data.started_at),
        ) / 1000
      : null,
  );
</script>

<details class="review" class:denied={data.status === "denied"}>
  <summary>
    <svg class="shield" viewBox="0 0 24 24" aria-hidden="true"
      ><path
        d="m12 3 8 3v6c0 5-8 9-8 9s-8-4-8-9V6z"
      />{#if data.status === "approved"}<path
          d="m8 12 3 3 5-6"
        />{:else if data.status === "denied"}<path
          d="m9 9 6 6m0-6-6 6"
        />{/if}</svg
    >
    {#if busy}<span class="spinner" aria-hidden="true"></span>{/if}
    <span class="label">{label}</span><span class="action">{data.title}</span>
    {#if duration !== null && Number.isFinite(duration)}<span class="duration"
        >{duration.toFixed(1)}s</span
      >{/if}
    <svg class="chevron" viewBox="0 0 20 20" aria-hidden="true"
      ><path d="m8 6 4 4-4 4" /></svg
    >
  </summary>
  <div class="body">
    <div class="assessment">
      {#if data.risk_level}<span>Risk <strong>{data.risk_level}</strong></span
        >{/if}
      {#if data.user_authorization}<span
          >User authorization <strong>{data.user_authorization}</strong></span
        >{/if}
      {#if showTool}<button class="tool-link" onclick={showTool}
          >View tool call <span aria-hidden="true">↗</span></button
        >{/if}
    </div>
    {#if data.risk_level || data.user_authorization}<p class="attribution">
        Assessed by the provider’s automatic reviewer.
      </p>{/if}
    {#if data.rationale}<p class="rationale">{data.rationale}</p>{/if}
    {#if details.command}<pre>{details.command}</pre>{/if}
    {#if details.cwd}<div class="path">{details.cwd}</div>{/if}
    {#if Array.isArray(details.files)}{#each details.files as path}<div
          class="path"
        >
          {path}
        </div>{/each}{/if}
    <details class="action-details">
      <summary>Action details</summary>
      <pre>{JSON.stringify(details, null, 2)}</pre>
    </details>
  </div>
</details>

<style>
  .review {
    color: var(--muted);
    font-size: 12px;
    min-width: 0;
  }
  summary {
    display: flex;
    align-items: center;
    gap: 9px;
    cursor: pointer;
    list-style: none;
    border-radius: 7px;
    padding: 5px 0;
  }
  summary::-webkit-details-marker {
    display: none;
  }
  summary:hover .label {
    color: var(--text);
  }
  summary:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 4px;
  }
  svg {
    width: 17px;
    height: 17px;
    fill: none;
    stroke: currentColor;
    stroke-width: 1.5;
    flex-shrink: 0;
  }
  .review:not(.denied) .shield {
    color: var(--muted);
  }
  .denied .shield {
    color: var(--danger);
  }
  .label {
    font-weight: 500;
  }
  .action {
    opacity: 0.8;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .duration {
    font-size: 11px;
    white-space: nowrap;
  }
  .chevron {
    width: 14px;
    height: 14px;
    transition: transform 0.15s;
  }
  .review[open] > summary .chevron {
    transform: rotate(90deg);
  }
  .body {
    margin: 9px 0 0 8px;
    padding: 4px 0 4px 18px;
    border-left: 1px solid var(--border);
  }
  .assessment {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px;
  }
  .assessment > span {
    border: 1px solid var(--panel-border);
    background: var(--hover-surface);
    border-radius: 6px;
    padding: 4px 7px;
    font-size: 11px;
  }
  strong {
    color: var(--text);
    font-weight: 500;
    margin-left: 4px;
    text-transform: capitalize;
  }
  .attribution {
    font-size: 11px;
    margin: 8px 0;
  }
  .rationale {
    color: var(--text);
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    line-height: 1.7;
    margin: 12px 0;
  }
  .tool-link {
    width: auto;
    min-height: 24px;
    padding: 2px 5px;
    background: transparent;
    border: 0;
    font-size: 11px;
    color: var(--muted);
  }
  .tool-link:hover {
    color: var(--text);
  }
  .path {
    font-family: monospace;
    font-size: 11px;
    overflow-wrap: anywhere;
    margin: 5px 0;
  }
  pre {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    font-size: 12px;
    line-height: 1.6;
    max-height: 280px;
    overflow: auto;
  }
  .action-details {
    margin-top: 10px;
    font-size: 11px;
  }
  .spinner {
    width: 10px;
    height: 10px;
    flex-shrink: 0;
    border: 1.5px solid var(--border);
    border-top-color: var(--muted);
    border-radius: 50%;
    animation: spin 1s linear infinite;
  }
  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .spinner {
      animation: none;
    }
    .chevron {
      transition: none;
    }
  }
  @media (max-width: 480px) {
    summary {
      flex-wrap: wrap;
      gap: 6px;
    }
    .action {
      max-width: 110px;
    }
  }
</style>
