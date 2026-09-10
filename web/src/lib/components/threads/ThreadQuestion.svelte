<script lang="ts">
  import type {
    ApprovalItem,
    Question,
    QuestionDraft,
  } from "$lib/thread-permissions.js";
  let {
    item,
    answer,
    editQuestion,
    compact = false,
  }: {
    item: ApprovalItem;
    answer: (
      item: ApprovalItem,
      decision: "answer",
      values: Record<string, string[]>,
    ) => void;
    editQuestion: (
      item: ApprovalItem,
      id: string,
      value: QuestionDraft,
    ) => void;
    compact?: boolean;
  } = $props();
  const uid = $props.id();
  const questions = $derived(item.data.questions ?? []);
  const pending = $derived(item.status === "pending");
  const locked = $derived(!item.available || item.busy);
  function draft(q: Question): QuestionDraft {
    return item.draft?.[q.id] ?? { selected: [], text: "" };
  }
  function values(q: Question) {
    const d = draft(q);
    return [
      ...new Set([...d.selected, ...(d.text.trim() ? [d.text.trim()] : [])]),
    ];
  }
  const valid = $derived(
    questions.length > 0 && questions.every((q) => values(q).length > 0),
  );
  function choose(q: Question, label: string) {
    const d = draft(q);
    const selected = q.multiple
      ? d.selected.includes(label)
        ? d.selected.filter((v) => v !== label)
        : [...d.selected, label]
      : [label];
    editQuestion(item, q.id, { selected, text: q.multiple ? d.text : "" });
  }
  function custom(q: Question, text: string) {
    editQuestion(item, q.id, {
      selected: q.multiple ? draft(q).selected : [],
      text,
    });
  }
</script>

<section class="question-card" class:compact aria-label="Question request">
  <div class="heading">
    <svg
      width="19"
      height="19"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      stroke-width="1.6"
      aria-hidden="true"
      ><path
        d="M21 11.5a8.5 8.5 0 0 1-8.5 8.5H4l-2 2V11.5A8.5 8.5 0 0 1 10.5 3h2a8.5 8.5 0 0 1 8.5 8.5Z"
      /><path d="M9.5 9a2.5 2.5 0 0 1 5 0c0 1.5-2.5 1.5-2.5 3m0 3v.1" /></svg
    >
    <strong
      >{pending
        ? "Your input"
        : item.status === "answered"
          ? "Answered"
          : "Question"}</strong
    >
    {#if !pending}<span class="state"
        >{item.status === "unconfirmed"
          ? "Answer unconfirmed"
          : item.status === "resolved"
            ? "No longer waiting"
            : item.status === "unavailable"
              ? "Unavailable"
              : ""}</span
      >{/if}
  </div>
  {#each questions as q (q.id)}
    <fieldset disabled={locked}>
      <legend>{q.text}</legend>
      {#if pending}
        {#if q.multiple}<p class="hint">Choose any that apply</p>{/if}
        <div class="options">
          {#each q.options as option}
            <label class:selected={draft(q).selected.includes(option.label)}>
              <input
                type={q.multiple ? "checkbox" : "radio"}
                name={uid + q.id}
                checked={draft(q).selected.includes(option.label)}
                onchange={() => choose(q, option.label)}
              />
              <span
                ><strong>{option.label}</strong>{#if option.description}<small
                    >{option.description}</small
                  >{/if}</span
              >
            </label>
          {/each}
        </div>
        <textarea
          aria-label={"Custom answer: " + q.text}
          rows="2"
          placeholder={q.options.length
            ? "Or type your own answer…"
            : "Type your answer…"}
          value={draft(q).text}
          oninput={(e) => custom(q, e.currentTarget.value)}></textarea>
      {:else}
        {#if item.answers?.[q.id]?.length}<p class="answer">
            {item.answers[q.id].join(", ")}
          </p>{/if}
      {/if}
    </fieldset>
  {/each}
  {#if item.error}<p class="error" role="alert">{item.error}</p>{/if}
  {#if pending}<div class="actions">
      <button
        class="primary"
        disabled={locked || !valid}
        onclick={() =>
          answer(
            item,
            "answer",
            Object.fromEntries(questions.map((q) => [q.id, values(q)])),
          )}
        >{item.busy ? "Sending…" : "Answer"}<svg
          width="15"
          height="15"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="1.8"
          aria-hidden="true"><path d="M5 12h14m-5-5 5 5-5 5" /></svg
        ></button
      >
    </div>{/if}
</section>

<style>
  .question-card {
    min-width: 0;
    padding: 16px;
    border: 1px solid var(--panel-border);
    border-radius: 14px;
    background: var(--hover-surface);
    font-size: 13px;
  }
  .compact {
    padding: 0;
    border: 0;
    background: transparent;
  }
  .heading {
    display: flex;
    gap: 9px;
    align-items: center;
    margin-bottom: 18px;
  }
  .heading svg {
    color: var(--muted);
    flex-shrink: 0;
  }
  strong {
    font-weight: 550;
  }
  .state {
    margin-left: auto;
    color: var(--muted);
    font-size: 11px;
  }
  fieldset {
    border: 0;
    padding: 0;
    margin: 0 0 18px;
    min-width: 0;
  }
  fieldset:last-of-type {
    margin-bottom: 0;
  }
  legend {
    font-weight: 550;
    line-height: 1.6;
    margin-bottom: 10px;
    overflow-wrap: anywhere;
  }
  .hint {
    margin: 0 0 9px;
    color: var(--muted);
    font-size: 11px;
  }
  .options {
    display: grid;
    gap: 7px;
  }
  label {
    display: flex;
    align-items: flex-start;
    gap: 10px;
    border: 1px solid var(--panel-border);
    border-radius: 9px;
    padding: 10px 12px;
    cursor: pointer;
    transition:
      background 0.15s,
      border-color 0.15s;
  }
  label:hover {
    background: var(--hover-surface);
  }
  label.selected {
    background: color-mix(in srgb, var(--accent, #91aecd) 12%, transparent);
    border-color: color-mix(
      in srgb,
      var(--accent, #91aecd) 55%,
      var(--panel-border)
    );
  }
  label input {
    width: 15px;
    height: 15px;
    min-height: 0;
    padding: 0;
    margin: 2px 0 0;
    flex-shrink: 0;
    accent-color: var(--accent, #91aecd);
  }
  label span {
    min-width: 0;
    overflow-wrap: anywhere;
  }
  small {
    display: block;
    color: var(--muted);
    line-height: 1.5;
    margin-top: 3px;
    font-size: 12px;
  }
  textarea {
    display: block;
    width: 100%;
    box-sizing: border-box;
    resize: vertical;
    margin-top: 10px;
    min-height: 58px;
    border: 1px solid var(--panel-border);
    border-radius: 9px;
    padding: 10px 12px;
    background: var(--surface);
    color: var(--text);
    font: inherit;
    line-height: 1.5;
  }
  textarea::placeholder {
    color: var(--muted);
  }
  .actions {
    display: flex;
    justify-content: flex-end;
    margin-top: 15px;
  }
  .actions button {
    display: inline-flex;
    align-items: center;
    gap: 10px;
    width: auto;
    min-height: 34px;
    padding: 7px 14px;
    font-size: 12px;
    border-radius: 8px;
  }
  .answer {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    margin: 0;
    color: var(--text);
  }
  .error {
    color: var(--danger);
  }
  @media (prefers-reduced-motion: reduce) {
    label {
      transition: none;
    }
  }
</style>
