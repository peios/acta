<script lang="ts">
  import { anchoredPopover } from "$lib/anchored-popover";
  import { useAccount } from "$lib/account-context";
  import { api } from "$lib/api";
  import {
    ThreadModelSettings,
    settingsForModel,
    sameModelSettings,
    type ModelState,
    type ModelSettings,
  } from "$lib/thread-model-settings.js";
  let {
    configuration,
    sequence,
    threadId,
    runId,
    available,
    editable,
    onbusy,
  }: {
    configuration: Record<string, unknown>;
    sequence: number;
    threadId: string;
    runId: string;
    available: boolean;
    editable: boolean;
    onbusy: (busy: boolean) => void;
  } = $props();
  const account = useAccount();
  const id = $props.id();
  let trigger = $state<HTMLButtonElement>();
  let open = $state(false);
  let session = $state<ThreadModelSettings>();
  let pickerState = $state<ModelState>({
    models: [],
    loading: false,
    pending: null,
    error: "",
  });
  $effect(() => {
    const thread = threadId,
      run = runId;
    const abort = new AbortController();
    const modelSession = new ThreadModelSettings({
      storage: localStorage,
      key: `acta:thread-settings:${account.account.id}:${thread}:${run}`,
      catalogueKey: `acta:thread-models:${account.account.id}:${thread}`,
      runId: run,
      uuid: () => crypto.randomUUID(),
      changed: (next) => {
        pickerState = next;
      },
      request: (body, command) =>
        api(`threads/${thread}/control${command ? `/${command}` : ""}`, body, {
          signal: AbortSignal.any([
            abort.signal,
            AbortSignal.timeout(body === undefined ? 5000 : 15000),
          ]),
        }),
    });
    session = modelSession;
    void modelSession.poll();
    const timer = setInterval(() => void modelSession.poll(), 1000);
    return () => {
      modelSession.close();
      abort.abort();
      clearInterval(timer);
    };
  });
  $effect(() => {
    session?.observe(configuration, sequence);
  });
  $effect(() => {
    onbusy(pickerState.loading || !!pickerState.pending);
  });
  const selected = $derived(pickerState.pending?.settings ?? configuration);
  const model = $derived(
    typeof configuration.model === "string" ? configuration.model : "",
  );
  const shownModel = $derived(
    typeof selected.model === "string" ? selected.model : "",
  );
  const effort = $derived(
    typeof selected.effort === "string" ? selected.effort : "",
  );
  const fast = $derived(selected.fast_mode === true);
  const capability = $derived(
    pickerState.models.find((m) => m.id === shownModel) ??
      pickerState.models.find((m) => m.resolved_model === shownModel),
  );
  const disabled = $derived(
    !editable || pickerState.loading || !!pickerState.pending,
  );
  let previewEffort = $state<number | null>(null);
  const efforts = $derived(
    capability?.efforts.length && capability.default_effort === ""
      ? [
          { id: "", description: "Let the provider choose reasoning effort." },
          ...capability.efforts,
        ]
      : (capability?.efforts ?? []),
  );
  const effortIndex = $derived(
    Math.max(
      0,
      efforts.findIndex((e) => e.id === effort),
    ),
  );
  const displayedIndex = $derived(previewEffort ?? effortIndex);
  const displayedEffort = $derived(efforts[displayedIndex]);
  const progress = $derived(
    efforts.length > 1 ? (displayedIndex / (efforts.length - 1)) * 100 : 0,
  );
  const effortLabel = (value: string) =>
    value === ""
      ? "Default"
      : ({ xhigh: "Extra high", max: "Maximum" }[value] ??
        value.charAt(0).toUpperCase() + value.slice(1));
  $effect(() => {
    if (!open || disabled) previewEffort = null;
  });
  function change(settings: ModelSettings) {
    if (!disabled && !sameModelSettings(settings, configuration))
      void session?.change(settings, sequence);
  }
</script>

<button
  class="model-trigger"
  bind:this={trigger}
  popovertarget={id}
  aria-label="Model settings"
  aria-haspopup="dialog"
  aria-expanded={open}
  title="Model settings"
>
  <span>{model || "Model settings"}</span>
  {#if configuration.fast_mode === true}<svg
      class="fast-icon"
      viewBox="0 0 20 20"
      aria-label="Fast mode"><path d="m11 2-7 9h5l-1 7 8-10h-5z" /></svg
    >{/if}
  <svg viewBox="0 0 20 20" aria-hidden="true"><path d="m6 8 4 4 4-4" /></svg>
</button>
<div
  {id}
  class="model-popup"
  popover="auto"
  role="dialog"
  aria-label="Model settings"
  onbeforetoggle={(event) => {
    open = event.newState === "open";
    if (open && available) void session?.load();
  }}
  use:anchoredPopover={() => ({
    anchor: trigger,
    width: 304,
    height: 500,
    align: "start",
  })}
>
  <header>
    <strong>Model</strong><span aria-live="polite"
      >{pickerState.loading
        ? "Loading…"
        : pickerState.pending?.state === "uncertain"
          ? "Unconfirmed"
          : pickerState.pending
            ? "Saving…"
            : "Next turn"}</span
    >
  </header>
  {#if !available}<p class="hint">Connect the harness to change settings.</p>
  {:else if !editable && !pickerState.pending}<p class="hint">
      Settings can be changed when this turn ends.
    </p>{/if}
  {#if pickerState.error}<p class="error" role="alert">
      {pickerState.error}
    </p>{/if}
  {#if pickerState.pending?.state === "uncertain"}<button
      class="retry"
      disabled={!available}
      onclick={() => void session?.submit()}>Check again</button
    >
  {:else if available && !pickerState.loading && !pickerState.models.length}<button
      class="retry"
      onclick={() => void session?.load()}>Load models</button
    >{/if}
  <div class="model-list" role="group" aria-label="Models">
    {#each pickerState.models as option (option.id)}
      <button
        class="model-option"
        class:selected={option.id === capability?.id}
        aria-pressed={option.id === capability?.id}
        title={option.description}
        {disabled}
        onclick={() => change(settingsForModel(option, configuration))}
      >
        <span>{option.name}</span>
        {#if option.id === capability?.id}<svg
            viewBox="0 0 20 20"
            aria-hidden="true"><path d="m4 10 4 4 8-8" /></svg
          >{/if}
      </button>
    {/each}
  </div>
  {#if capability}
    <div class="tuning">
      <div class="tuning-heading">
        <label for={`${id}-effort`}
          >Effort <span class="effort-value"
            >{displayedEffort
              ? effortLabel(displayedEffort.id)
              : "Not applicable"}</span
          ></label
        >
        <button
          class="speed"
          class:enabled={fast}
          aria-label="Fast mode"
          aria-pressed={fast}
          disabled={disabled || !capability.fast_mode}
          title={capability.fast_mode
            ? `Fast mode${fast ? " on" : " off"} · ${capability.fast_description || "Faster responses, increased usage"}`
            : "Fast mode is not available for this model"}
          onclick={() =>
            change({ model: shownModel, effort, fast_mode: !fast })}
        >
          <svg viewBox="0 0 20 20" aria-hidden="true"
            ><path d="m11 2-7 9h5l-1 7 8-10h-5z" /></svg
          >
        </button>
      </div>
      <div class="effort-slider" style={`--progress:${progress}%`}>
        <div class="slider-track" aria-hidden="true">
          <div class="slider-fill"></div>
          {#each efforts as level, index (level.id)}<i
              class:reached={index <= displayedIndex}
              style={`left:${efforts.length > 1 ? (index / (efforts.length - 1)) * 100 : 0}%`}
            ></i>{/each}
        </div>
        <input
          id={`${id}-effort`}
          type="range"
          min="0"
          max={Math.max(1, efforts.length - 1)}
          step="1"
          value={displayedIndex}
          aria-valuetext={displayedEffort
            ? effortLabel(displayedEffort.id)
            : "Not applicable"}
          disabled={!editable || efforts.length < 2}
          aria-disabled={disabled}
          onpointercancel={() => (previewEffort = null)}
          oninput={(event) => {
            if (disabled) {
              event.currentTarget.value = String(effortIndex);
              return;
            }
            previewEffort = Number(event.currentTarget.value);
          }}
          onchange={(event) => {
            if (disabled) {
              event.currentTarget.value = String(effortIndex);
              return;
            }
            const level = efforts[Number(event.currentTarget.value)];
            if (level)
              change({ model: shownModel, effort: level.id, fast_mode: fast });
            previewEffort = null;
          }}
        />
      </div>
      <p class="effort-description">
        {displayedEffort?.description ||
          "This model does not expose an effort setting."}
      </p>
    </div>
  {/if}
</div>

<style>
  .hint,
  .error {
    font-size: 12px;
    line-height: 1.5;
    margin: 8px 0;
    color: var(--muted);
  }
  .error {
    color: var(--error-text);
  }
  .retry {
    width: auto;
    font-size: 12px;
    padding: 5px 9px;
  }
  .model-trigger {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    width: auto;
    min-width: 0;
    min-height: 30px;
    padding: 4px 6px;
    border: 0;
    border-radius: 7px;
    background: transparent;
    color: var(--muted);
    font-size: 12px;
  }
  .model-trigger:hover,
  .model-trigger[aria-expanded="true"] {
    background: var(--surface);
    color: var(--text);
  }
  .model-trigger span {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  svg {
    width: 15px;
    height: 15px;
    flex-shrink: 0;
    fill: none;
    stroke: currentColor;
    stroke-width: 1.5;
    stroke-linecap: round;
    stroke-linejoin: round;
  }
  .fast-icon {
    width: 13px;
  }
  .model-popup {
    position: fixed;
    inset: auto;
    margin: 0;
    padding: 14px;
    border: 1px solid var(--panel-border);
    border-radius: 12px;
    background: var(--surface);
    color: var(--text);
    box-shadow: 0 12px 36px #0003;
    overflow: auto;
  }
  header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    margin-bottom: 12px;
    font-size: 13px;
  }
  header span {
    color: var(--muted);
    font-size: 11px;
  }

  .model-list {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .model-option {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    width: 100%;
    min-height: 34px;
    padding: 7px 9px;
    border: 0;
    border-radius: 7px;
    background: transparent;
    color: var(--muted);
    text-align: left;
    font-size: 13px;
    font-weight: 400;
    box-shadow: none;
    transition:
      background 130ms,
      color 130ms;
  }
  .model-option:hover {
    background: color-mix(in srgb, var(--accent) 7%, transparent);
    color: var(--text);
  }
  .model-option.selected {
    background: color-mix(in srgb, var(--accent) 11%, transparent);
    color: var(--text);
  }
  .model-option svg {
    color: var(--accent);
  }
  .tuning {
    margin-top: 12px;
    padding: 12px 4px 0;
    border-top: 1px solid var(--panel-border);
  }
  .tuning-heading {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
  }
  .tuning-heading label {
    display: flex;
    align-items: center;
    gap: 9px;
    margin: 0;
    font-size: 12px;
    font-weight: 500;
  }
  .effort-value {
    color: var(--muted);
    font-weight: 400;
  }
  .speed {
    display: grid;
    place-items: center;
    width: 28px;
    height: 28px;
    min-height: 0;
    padding: 5px;
    border: 0;
    border-radius: 7px;
    background: transparent;
    color: var(--muted);
    box-shadow: none;
    transition:
      background 160ms,
      color 160ms,
      transform 160ms;
  }
  .speed:hover {
    background: color-mix(in srgb, var(--accent) 8%, transparent);
  }
  .speed:active {
    transform: scale(0.92);
  }
  .speed.enabled {
    color: #c99739;
    background: color-mix(in srgb, #c99739 12%, transparent);
  }
  .speed.enabled svg {
    fill: color-mix(in srgb, currentColor 20%, transparent);
  }
  .speed:disabled {
    opacity: 0.35;
  }
  .effort-slider {
    position: relative;
    height: 32px;
    margin: 4px 0;
  }
  .slider-track {
    position: absolute;
    height: 3px;
    left: 8px;
    right: 8px;
    top: 14px;
    border-radius: 8px;
    background: color-mix(in srgb, var(--muted) 25%, transparent);
  }
  .slider-fill {
    height: 100%;
    width: var(--progress);
    border-radius: inherit;
    background: var(--accent);
  }
  .slider-track i {
    position: absolute;
    top: 50%;
    width: 5px;
    height: 5px;
    border-radius: 50%;
    background: color-mix(in srgb, var(--muted) 65%, var(--surface));
    transform: translate(-50%, -50%);
  }
  .slider-track i.reached {
    background: var(--accent);
  }
  .effort-slider input {
    position: relative;
    display: block;
    appearance: none;
    -webkit-appearance: none;
    width: 100%;
    height: 32px;
    margin: 0;
    padding: 0;
    border: 0;
    background: transparent;
    box-shadow: none;
    cursor: pointer;
  }
  .effort-slider input::-webkit-slider-runnable-track {
    height: 3px;
    background: transparent;
  }
  .effort-slider input::-moz-range-track {
    height: 3px;
    background: transparent;
  }
  .effort-slider input::-webkit-slider-thumb {
    appearance: none;
    width: 16px;
    height: 16px;
    margin-top: -6.5px;
    border: 3px solid var(--surface);
    border-radius: 50%;
    background: var(--accent);
    box-shadow:
      0 0 0 1px color-mix(in srgb, var(--accent) 40%, transparent),
      0 2px 5px #0003;
    transition: box-shadow 130ms;
  }
  .effort-slider input::-moz-range-thumb {
    box-sizing: border-box;
    width: 16px;
    height: 16px;
    border: 3px solid var(--surface);
    border-radius: 50%;
    background: var(--accent);
    box-shadow: 0 0 0 1px var(--accent);
  }
  .effort-slider input:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
    border-radius: 6px;
  }
  .effort-slider input:disabled,
  .effort-slider input[aria-disabled="true"] {
    cursor: default;
    opacity: 0.5;
  }
  .effort-description {
    min-height: 32px;
    margin: 0;
    color: var(--muted);
    font-size: 11px;
    line-height: 1.5;
  }
  @media (prefers-reduced-motion: reduce) {
    .model-option,
    .speed {
      transition: none;
    }
  }
</style>
