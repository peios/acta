<script lang="ts">
  import type { UserMessage, FeedItem } from "../../src/lib/thread-feed.js";
  import { onMount, tick } from "svelte";
  import ThreadTranscript from "../../src/lib/components/threads/ThreadTranscript.svelte";
  import "../../src/lib/styles.css";
  const row = (i: number): UserMessage => ({
    kind: "user-message",
    id: `row-${i}`,
    frame: {
      thread_id: "scroll-test",
      run_id: "run",
      sequence: i,
      kind: "message/user",
      provider: "test",
      output_index: 0,
      raw_json: "{}",
      data: {},
      received_at: "2026-09-09T10:00:00Z",
    },
    text: `Message ${i}\n${"Readable history. ".repeat(12)}`,
    completed: true,
  });
  let items = $state<FeedItem[]>(Array.from({ length: 40 }, (_, i) => row(i)));
  let initialLoading = $state(false);
  let loadingOlder = $state(false),
    hasMore = $state(false);
  let next = 40;
  let groupedPages = 0,
    olderCalls = 0;
  let noProgress = false;
  const thought = (i: number): FeedItem => ({
    kind: "thinking",
    id: `thought-${i}`,
    frame: row(i).frame,
    turn: "old-turn",
    text: "",
    tokens: null,
    started: null,
    ended: null,
    completed: true,
    interrupted: false,
  });
  async function older() {
    olderCalls++;
    loadingOlder = true;
    await tick();
    await new Promise((r) => setTimeout(r, 30));
    if (noProgress) {
      loadingOlder = false;
      await tick();
      return;
    }
    if (groupedPages > 0) {
      items = [thought(-groupedPages), ...items];
      groupedPages--;
      hasMore = groupedPages > 0;
      loadingOlder = false;
      await tick();
      return;
    }
    items = [...Array.from({ length: 10 }, (_, i) => row(-10 + i)), ...items];
    hasMore = false;
    loadingOlder = false;
    await tick();
  }
  onMount(() => {
    initialLoading = false;
    Object.assign(window, {
      scrollFixture: {
        groupedHistory: async (fail = false) => {
          noProgress = fail;
          items = [
            thought(0),
            ...Array.from({ length: 15 }, (_, i) => row(i + 1)),
          ];
          // Give the later user message a different turn so old thinking folds.
          items.at(-1)!.frame.data = { turn_id: "latest-turn" };
          groupedPages = 3;
          olderCalls = 0;
          hasMore = true;
          await tick();
        },
        olderCalls: () => olderCalls,
        replace: async (data: FeedItem[]) => {
          items = data;
          await tick();
        },
        poll: async () => {
          items = structuredClone($state.snapshot(items));
          await tick();
        },
        append: async () => {
          items = [...items, row(next++)];
          await tick();
        },
        grow: async (index = items.length - 1) => {
          items = items.map((item, i) =>
            i === index
              ? {
                  ...item,
                  text:
                    ("text" in item ? item.text : "") +
                    "\n" +
                    "Streamed text. ".repeat(40),
                }
              : item,
          );
          await tick();
        },
        shorter: async () => {
          items = items.slice(-15);
          await tick();
        },
        older,
        enableOlder: async () => {
          hasMore = true;
          await tick();
        },
        stress: async () => {
          next = 1000;
          items = Array.from({ length: 1000 }, (_, i) => row(i));
          await tick();
        },
        reset: async () => {
          next = 40;
          items = Array.from({ length: 40 }, (_, i) => row(i));
          await tick();
        },
      },
    });
  });
</script>

<div class="test">
  <ThreadTranscript
    {items}
    {initialLoading}
    approvals={[]}
    answerApproval={() => {}}
    showDebug={false}
    threadId="scroll-test"
    {loadingOlder}
    {hasMore}
    loadOlder={older}
  />
</div>

<style>
  .test {
    height: 520px;
    width: min(720px, 100vw);
    display: flex;
    flex-direction: column;
    padding: 16px;
    box-sizing: border-box;
  }
  :global(body) {
    margin: 0;
  }
</style>
