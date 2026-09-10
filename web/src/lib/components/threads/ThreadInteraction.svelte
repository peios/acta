<script lang="ts">
  import type { ApprovalItem, QuestionDraft } from "$lib/thread-permissions.js";
  import ThreadApproval from "./ThreadApproval.svelte";
  import ThreadQuestion from "./ThreadQuestion.svelte";
  let {
    item,
    answer,
    editQuestion,
    compact = false,
  }: {
    item: ApprovalItem;
    answer: (
      item: ApprovalItem,
      decision: "approve" | "deny" | "answer",
      values?: Record<string, string[]>,
    ) => void;
    editQuestion: (
      item: ApprovalItem,
      id: string,
      value: QuestionDraft,
    ) => void;
    compact?: boolean;
  } = $props();
</script>

{#if item.data.lane_name}<div class="lane-attribution">
    {item.data.lane_name}
  </div>{/if}

{#if item.frame.kind === "question/request"}
  <ThreadQuestion {item} {answer} {editQuestion} {compact} />
{:else}
  <ThreadApproval {item} {answer} {compact} />
{/if}

<style>
  .lane-attribution {
    font-size: 11px;
    color: var(--muted);
    margin-bottom: 6px;
  }
</style>
