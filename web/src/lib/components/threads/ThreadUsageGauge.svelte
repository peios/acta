<script lang="ts">
  import type { UsageGauge } from "$lib/thread-usage.js";
  import { anchoredPopover } from "$lib/anchored-popover";
  import RelativeTime from "$lib/components/RelativeTime.svelte";
  let { gauge }: { gauge: UsageGauge } = $props();
  const id = $props.id();
  let trigger = $state<HTMLButtonElement>();
  let open = $state(false);
  const percent = $derived(
    gauge.percent === null
      ? "Not reported"
      : `${Math.round(gauge.percent * 10) / 10}% used`,
  );
</script>

<button
  class="gauge"
  class:warning={gauge.percent !== null && gauge.percent >= 80}
  class:exhausted={gauge.percent !== null && gauge.percent >= 100}
  bind:this={trigger}
  popovertarget={id}
  aria-haspopup="dialog"
  aria-expanded={open}
  aria-label={`${gauge.title}: ${percent}`}
  title={`${gauge.title}: ${percent}`}
>
  <svg viewBox="0 0 32 32" aria-hidden="true">
    <circle class="track" cx="16" cy="16" r="12" />
    {#if gauge.percent !== null}<circle
        class="fill"
        cx="16"
        cy="16"
        r="12"
        pathLength="100"
        stroke-dasharray={`${Math.min(100, gauge.percent)} 100`}
      />{/if}
    <text x="16" y="16"
      >{gauge.percent === null ? "?" : Math.round(gauge.percent)}</text
    >
  </svg>
  <span>{gauge.label}</span>
</button>
<div
  {id}
  class="usage-popup"
  popover="auto"
  role="dialog"
  aria-label={gauge.title}
  onbeforetoggle={(event) => (open = event.newState === "open")}
  use:anchoredPopover={() => ({
    anchor: trigger,
    width: 285,
    height: 360,
    align: "end",
  })}
>
  <strong>{gauge.title}</strong>
  <p class="percentage">{percent}</p>
  <dl>
    {#each gauge.details as detail}<div>
        <dt>{detail.label}</dt>
        <dd>{detail.value}</dd>
      </div>{/each}
  </dl>
  {#if gauge.reportedAt}<p class="reported">
      Last reported <RelativeTime value={gauge.reportedAt} />
    </p>{/if}
</div>

<style>
  .gauge {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 1px;
    width: 44px;
    min-height: 49px;
    padding: 2px;
    border: 0;
    border-radius: 7px;
    background: transparent;
    color: var(--muted);
  }
  .gauge:hover,
  .gauge[aria-expanded="true"] {
    background: var(--hover-surface);
  }
  .gauge.warning {
    color: #b98632;
  }
  .gauge.exhausted {
    color: var(--danger);
  }
  svg {
    width: 36px;
    height: 36px;
    overflow: visible;
  }
  circle {
    fill: none;
    stroke: currentColor;
    stroke-width: 2.5;
  }
  .track {
    opacity: 0.18;
  }
  .fill {
    transform: rotate(-90deg);
    transform-origin: center;
    stroke-linecap: round;
    transition: stroke-dasharray 220ms ease;
  }
  text {
    fill: currentColor;
    text-anchor: middle;
    dominant-baseline: central;
    font-family: inherit;
    font-size: 9px;
    font-variant-numeric: tabular-nums;
  }
  .gauge > span {
    font-size: 10px;
    line-height: 12px;
  }
  .usage-popup {
    position: fixed;
    inset: auto;
    margin: 0;
    padding: 16px;
    border: 1px solid var(--panel-border);
    border-radius: 12px;
    background: var(--surface);
    color: var(--text);
    box-shadow: 0 12px 36px #0003;
    overflow: auto;
    font-size: 13px;
  }
  strong {
    font-weight: 550;
  }
  .percentage {
    font-size: 20px;
    margin: 10px 0 14px;
    font-variant-numeric: tabular-nums;
  }
  dl {
    margin: 0;
  }
  dl > div {
    display: flex;
    justify-content: space-between;
    gap: 16px;
    padding: 6px 0;
    font-size: 12px;
  }
  dt {
    color: var(--muted);
  }
  dd {
    margin: 0;
    text-align: right;
    overflow-wrap: anywhere;
    min-width: 0;
  }
  .reported {
    font-size: 11px;
    color: var(--muted);
    margin: 14px 0 0;
  }
  @media (prefers-reduced-motion: reduce) {
    .fill {
      transition: none;
    }
  }
</style>
