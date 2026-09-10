<script lang="ts">
  import { onMount } from "svelte";
  import ThreadTranscript from "$lib/components/threads/ThreadTranscript.svelte";
  import type { FeedItem } from "$lib/thread-feed.js";
  import type { ThreadFrame } from "$lib/threads.svelte";
  const frame = (i: number) =>
    ({
      thread_id: "review",
      run_id: "review",
      sequence: i,
      output_index: 0,
      kind: "message/assistant",
      data: {},
      raw_json: "",
      provider: "claude",
      received_at: new Date().toISOString(),
      schema_version: 1,
      occurred_at: null,
    }) as ThreadFrame;
  const message = (
    i: number,
  ): Extract<FeedItem, { kind: "assistant-message" }> => ({
    kind: "assistant-message",
    id: String(i),
    frame: frame(i),
    text: `Message ${i}. This is older conversation text for checking scroll position.\n\nAnother paragraph to make each message span several lines.`,
    completed: true,
    phase: null,
  });
  let items = $state<FeedItem[]>(
    Array.from({ length: 45 }, (_, i) => message(i)),
  );
  let updates = $state(0),
    streaming = $state(true);
  onMount(() => {
    const timer = setInterval(() => {
      if (!streaming) return;
      updates++;
      items = [
        ...items.slice(0, -1),
        {
          ...message(44),
          text:
            "Streaming response.\n\n" +
            Array.from(
              { length: updates },
              (_, i) => `Update ${i}: More streamed text.`,
            ).join("\n\n"),
        },
      ];
    }, 120);
    return () => clearInterval(timer);
  });
  async function older() {
    items = [
      ...Array.from({ length: 10 }, (_, i) => message(-10 + i)),
      ...items,
    ];
  }
</script>

<div class="review">
  <header>
    Scroll review · Updates: {updates}
    <button onclick={() => (streaming = !streaming)}
      >{streaming ? "Pause streaming" : "Resume streaming"}</button
    >
  </header>
  <ThreadTranscript
    {items}
    threadId="review"
    approvals={[]}
    answerApproval={() => {}}
    showDebug={false}
    hasMore={items[0]?.id === "0"}
    loadOlder={older}
  />
</div>

<style>
  .review {
    height: 100dvh;
    display: flex;
    flex-direction: column;
    padding: 20px;
    box-sizing: border-box;
  }
  header {
    flex: none;
    padding: 12px;
    display: flex;
    gap: 20px;
    align-items: center;
  }
</style>
