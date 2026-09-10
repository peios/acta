<script lang="ts">
  import MemoryViewer from "$lib/components/memories/MemoryViewer.svelte";
  import { ReferenceResolver } from "$lib/thread-reference-resolver.js";
  import type { MemoryReference } from "$lib/thread-memory-references.js";
  import { provideThreadMemories } from "$lib/thread-memory-context";

  import ThreadTaskViewer from "$lib/components/tasks/ThreadTaskViewer.svelte";
  import {
    TaskReferenceResolver,
    taskUUID,
  } from "$lib/thread-task-references.js";
  import { provideThreadTasks } from "$lib/thread-task-context";
  import type { Task } from "$lib/tasks";
  import {
    ThreadPermissions,
    approvalItems,
    type PermissionState,
    type ApprovalItem,
  } from "$lib/thread-permissions.js";
  import ThreadLanes from "$lib/components/threads/ThreadLanes.svelte";
  import ThreadHeader from "$lib/components/threads/ThreadHeader.svelte";
  import ThreadTranscript from "$lib/components/threads/ThreadTranscript.svelte";
  import {
    ThreadSession,
    type ThreadSessionState,
  } from "$lib/thread-session.js";
  import { page } from "$app/state";
  import { goto } from "$app/navigation";
  import { api } from "$lib/api";
  import { useNotifications } from "$lib/notifications.svelte";
  const notifications = useNotifications();
  import { useThreads } from "$lib/threads.svelte";
  import { withPendingMessage } from "$lib/thread-feed.js";
  import ThreadWorking from "$lib/components/threads/ThreadWorking.svelte";
  import ThreadComposer from "$lib/components/threads/ThreadComposer.svelte";
  import { threadUsage } from "$lib/thread-usage.js";
  import { ThreadSender, type SendingState } from "$lib/thread-sending.js";
  import { useAccount } from "$lib/account-context";
  const account = useAccount();
  // Query-only task navigation must not restart the chat session or its outbox.
  const currentThreadID = $derived(page.params.id!);
  let availableWidth = $state(0);
  let taskResolver = $state(
    new TaskReferenceResolver((id, signal) =>
      api<Task>(`tasks/${id}`, undefined, { signal }),
    ),
  );
  $effect(() => {
    account.account.id;
    const resolver = new TaskReferenceResolver((id, signal) =>
      api<Task>(`tasks/${id}`, undefined, { signal }),
    );
    taskResolver = resolver;
    return () => resolver.close();
  });
  let memoryResolver = $state(
    new ReferenceResolver<MemoryReference>((id, signal) =>
      api<MemoryReference>(`memories/${id}`, undefined, { signal }),
    ),
  );
  $effect(() => {
    account.account.id;
    const resolver = new ReferenceResolver<MemoryReference>((id, signal) =>
      api<MemoryReference>(`memories/${id}`, undefined, { signal }),
    );
    memoryResolver = resolver;
    return () => resolver.close();
  });
  const selectedMemory = $derived(
    taskUUID(page.url.searchParams.get("memory"))
      ? page.url.searchParams.get("memory")!
      : "",
  );
  function openMemory(id: string) {
    const url = new URL(page.url);
    if (id) {
      url.searchParams.set("memory", id);
      url.searchParams.delete("task");
    } else url.searchParams.delete("memory");
    void goto(url, { noScroll: true, keepFocus: true });
  }
  provideThreadMemories({
    resolve: (id) => memoryResolver.resolve(id),
    open: openMemory,
  });
  const selectedTask = $derived(
    taskUUID(page.url.searchParams.get("task"))
      ? page.url.searchParams.get("task")!
      : "",
  );
  function openTask(id: string) {
    const url = new URL(page.url);
    if (id) {
      url.searchParams.set("task", id);
      url.searchParams.delete("memory");
    } else url.searchParams.delete("task");
    void goto(url, { noScroll: true, keepFocus: true });
  }
  provideThreadTasks({
    resolve: (id) => taskResolver.resolve(id),
    open: openTask,
  });

  let selectedLane = $state("");
  const requestedLane = $derived(page.url.searchParams.get("lane") || "");
  $effect(() => {
    currentThreadID;
    selectedLane = requestedLane;
  });
  function openLane(id: string) {
    selectedLane = id;
    const url = new URL(page.url);
    if (id) url.searchParams.set("lane", id);
    else url.searchParams.delete("lane");
    void goto(url, { noScroll: true, keepFocus: true });
  }
  $effect(() => {
    if (!notifications) return;
    notifications.viewing = {
      thread: currentThreadID,
      lane: selectedLane,
      sequence: sessionState.cursor,
      ready: !sessionState.initialLoading && !sessionState.error,
    };
    return () => {
      notifications.viewing = null;
    };
  });
  $effect(() => {
    session?.setLane(selectedLane);
  });
  let sender = $state<ThreadSender | null>(null);
  let sending = $state<SendingState>({ draft: "", pending: null, error: "" });
  $effect(() => {
    const id = currentThreadID;
    const lane = selectedLane;
    const key = `acta2:thread-draft:${account.account.id}:${id}${lane ? ":" + lane : ""}`;
    const abort = new AbortController();
    const session = new ThreadSender({
      storage: localStorage,
      key,
      uuid: () => crypto.randomUUID(),
      changed: (state) => {
        sending = state;
      },
      request: (body, command) =>
        api(`threads/${id}/control${command ? `/${command}` : ""}`, body, {
          signal: AbortSignal.any([
            abort.signal,
            AbortSignal.timeout(body === undefined ? 5000 : 15000),
          ]),
        }),
    });
    sender = session;
    void session.poll();
    const timer = setInterval(() => void session.poll(), 1000);
    return () => {
      session.close();
      abort.abort();
      clearInterval(timer);
    };
  });
  $effect(() => {
    // The class outbox is not reactive. Subscribe to its published pending
    // state so an initially empty outbox cannot skip tracking array appends.
    if (sending.pending) sender?.observe(frames);
  });
  const sendLocked = $derived(
    !!sending.pending && sending.pending.state !== "rejected",
  );
  function send() {
    if (
      canSend &&
      !settingsBusy &&
      !permissionState.modePending &&
      !needsApproval &&
      thread
    )
      void sender?.send(thread.run_id, sessionState.revision);
  }
  const view = useThreads();
  async function deleteThread() {
    const id = currentThreadID;
    await api(`threads/${id}/delete`, {});
    view.forget(id);
    localStorage.removeItem(`acta2:thread-draft:${account.account.id}:${id}`);
    if (currentThreadID === id)
      await goto("/my-agents", { replaceState: true });
  }
  const thread = $derived(view.items.find((t) => t.id === currentThreadID));
  let session = $state<ThreadSession | null>(null);
  let sessionState = $state.raw<ThreadSessionState>({
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
  });
  const laneCurrent = $derived(
    selectedLane
      ? (sessionState.current.lanes?.[selectedLane] ?? {})
      : sessionState.current,
  );
  const agents = $derived(Object.values(sessionState.current.agents ?? {}));
  const selectedAgent = $derived(
    agents.find((a) => a.data?.lane_id === selectedLane),
  );
  const pendingFrames = $derived(
    [
      sessionState.current,
      ...Object.values(sessionState.current.lanes ?? {}),
    ].flatMap((c) => Object.values(c.pending ?? {})),
  );
  const currentFrames = $derived(Object.values(laneCurrent.frames ?? {}));
  const itemFrames = $derived(sessionState.items.map((i) => i.payload.frame));
  const frames = $derived([...currentFrames, ...itemFrames]);
  const approvalFrames = $derived(
    [
      ...pendingFrames,
      ...itemFrames.filter((f) =>
        ["approval/request", "question/request"].includes(f.kind),
      ),
    ].map((f) => ({
      ...f,
      approval_resolved: !pendingFrames.some(
        (p) =>
          (p.data?.approval_id ?? p.data?.question_id) ===
          (f.data?.approval_id ?? f.data?.question_id),
      ),
      data: {
        ...f.data,
        lane_name: f.lane_id
          ? (agents.find((a) => a.data?.lane_id === f.lane_id)?.data?.name ??
            "Subagent")
          : "Main",
      },
    })),
  );

  const error = $derived(sessionState.error);
  const controlError = $derived(sessionState.controlError);
  const pending = $derived(sessionState.pending);
  const busy = $derived(sessionState.busy);
  $effect(() => {
    const current = new ThreadSession({
      id: currentThreadID,
      uuid: () => crypto.randomUUID(),
      request: (path, body, signal) => api(path, body, { signal }),
      refreshThreads: (signal) => view.refresh(signal),
      changed: (state) => {
        sessionState = state;
      },
    });
    session = current;
    current.start();
    return () => current.close();
  });
  const permissionRun = $derived(thread?.run_id ?? "");
  let permissionSession = $state<ThreadPermissions>();
  let permissionState = $state.raw<PermissionState>({
    results: {},
    busy: {},
    errors: {},
    checked: {},
    modePending: "",
    modeError: "",
  });
  $effect(() => {
    const id = currentThreadID,
      runId = permissionRun;
    const abort = new AbortController();
    const current = new ThreadPermissions({
      runId,
      uuid: () => crypto.randomUUID(),
      storage: localStorage,
      key: `acta2:thread-permissions:${account.account.id}:${id}:${runId}`,
      changed: (state) => {
        permissionState = state;
      },
      request: (body, command) =>
        api(`threads/${id}/control${command ? `/${command}` : ""}`, body, {
          signal: AbortSignal.any([abort.signal, AbortSignal.timeout(15000)]),
        }),
    });
    permissionSession = current;
    const timer = setInterval(() => void current.poll(), 1000);
    return () => {
      current.close();
      abort.abort();
      clearInterval(timer);
    };
  });
  $effect(() => {
    permissionSession?.observe(approvalFrames);
  });
  const approvals = $derived(
    approvalItems(approvalFrames, permissionState, thread),
  );
  const needsApproval = $derived(
    approvals.some(
      (a) =>
        a.status === "pending" &&
        (a.frame.lane_id ?? "") === selectedLane &&
        a.data.blocking !== false,
    ),
  );
  function answerApproval(
    item: ApprovalItem,
    decision: "approve" | "deny" | "answer",
    values?: Record<string, string[]>,
  ) {
    void permissionSession?.answer(item, decision, values);
  }
  function editQuestion(
    item: ApprovalItem,
    id: string,
    value: { selected: string[]; text: string },
  ) {
    permissionSession?.draftQuestion(item, id, value);
  }
  let showDebug = $state(false);
  $effect(() => {
    session?.setDebug(showDebug);
  });
  const displayRun = $derived(
    selectedLane ? laneCurrent.run_id : thread?.run_id,
  );
  const configurationFrame = $derived(
    currentFrames.findLast(
      (frame) =>
        frame.kind === "thread/configuration" && frame.run_id === displayRun,
    ),
  );
  const configuration = $derived(configurationFrame?.data ?? {});
  const feed = $derived(
    withPendingMessage(
      sessionState.items.map((i) => i.payload),
      sending.pending,
      frames.at(-1),
    ),
  );
  const usage = $derived(threadUsage(currentFrames, displayRun));
  const working = $derived(
    !error &&
      thread?.state === "running" &&
      !!thread.connection_id &&
      currentFrames.find(
        (f) => f.run_id === thread.run_id && f.kind === "thread/status",
      )?.data?.status === "active",
  );
  const backgroundCount = $derived(
    !error &&
      thread?.state === "running" &&
      thread.connection_id &&
      laneCurrent.run_id === thread.run_id
      ? Object.keys(laneCurrent.background ?? {}).length
      : 0,
  );
  let settingsBusy = $state(false);
  const currentStatus = $derived(
    currentFrames.findLast(
      (f) => f.run_id === thread?.run_id && f.kind === "thread/status",
    )?.data?.status,
  );
  const canSend = $derived(
    !selectedLane &&
      !error &&
      !busy &&
      !pending &&
      thread?.state === "running" &&
      !!thread.connection_id &&
      ["idle", "active"].includes(String(currentStatus)),
  );
</script>

<div class="thread-workbench" bind:clientWidth={availableWidth}>
  <div class="thread-page">
    {#if thread}
      <ThreadHeader
        {thread}
        onrename={async (name) => {
          if (!session || sessionState.busy || sessionState.pending)
            throw new Error("A thread operation is already pending.");
          await session.control("rename", undefined, name);
          if (sessionState.controlError)
            throw new Error(sessionState.controlError);
        }}
        ondelete={deleteThread}
        {usage}
        bind:showDebug
        {busy}
        {settingsBusy}
        pending={!!pending}
        control={(action) => void session?.control(action)}
      />
    {/if}
    <ThreadLanes
      runId={thread?.run_id}
      {agents}
      selected={selectedLane}
      onselect={openLane}
      connected={!!thread?.connection_id && thread?.state === "running"}
    />
    {#if pending}<p role="status">
        {{
          kill: "Kill",
          resume: "Resume",
          interrupt: "Stop",
          rename: "Rename",
        }[pending.action]} requested…
      </p>{/if}
    {#if controlError}<p class="notice error" role="alert">
        {controlError}
      </p>{/if}
    {#if error}<p class="notice error" role="alert">{error}</p>{/if}
    <ThreadTranscript
      items={feed}
      latestTurnStart={laneCurrent.frames?.["turn/started"]}
      hasMore={sessionState.hasMore}
      loadingOlder={sessionState.loadingOlder}
      initialLoading={sessionState.initialLoading}
      loadOlder={() => session?.loadOlder() ?? Promise.resolve()}
      threadId={currentThreadID + ":" + selectedLane}
      {openLane}
      {thread}
      {showDebug}
      {approvals}
      {answerApproval}
      {editQuestion}
    />
    <ThreadWorking
      compacting={!!thread?.connection_id &&
        thread?.state === "running" &&
        laneCurrent.run_id === thread?.run_id &&
        laneCurrent.frames?.["context/compaction"]?.data?.state ===
          "in_progress"}
      active={working || needsApproval}
      waiting={needsApproval}
      question={approvals.some(
        (a) =>
          a.status === "pending" &&
          (a.frame.lane_id ?? "") === selectedLane &&
          a.data.blocking !== false &&
          a.frame.kind === "question/request",
      )}
      background={backgroundCount}
    />
    {#if sending.error}<p class="notice error" role="alert">
        {sending.error}
      </p>{/if}
    {#if sending.pending?.state === "rejected" || sending.pending?.state === "uncertain"}
      <div class="send-recovery" role="status">
        <span
          >{sending.pending.error || "Message delivery is unconfirmed."}</span
        >
        <button
          disabled={!thread?.connection_id}
          onclick={() => {
            if (sending.pending?.state === "rejected") send();
            else void sender?.submit();
          }}
          >{sending.pending.state === "rejected"
            ? "Retry"
            : "Check again"}</button
        >
      </div>
    {/if}
    {#key `${currentThreadID}:${selectedLane}`}{#if selectedLane}<p
          class="lane-hint"
        >
          Subagent conversation · direct messages are unavailable from this
          provider.
        </p>{/if}
      <ThreadComposer
        stopAvailable={!!thread?.connection_id && thread?.state === "running"}
        {permissionState}
        {approvals}
        {answerApproval}
        {editQuestion}
        changePermission={(mode) => void permissionSession?.mode(mode)}
        {configuration}
        configurationSequence={configurationFrame?.sequence ?? 0}
        threadId={thread?.id ?? currentThreadID}
        runId={thread?.run_id ?? ""}
        available={!selectedLane &&
          !!thread?.connection_id &&
          thread?.state === "running"}
        settingsEditable={!!canSend &&
          currentStatus === "idle" &&
          !sendLocked &&
          !settingsBusy &&
          !needsApproval}
        permissionsEditable={!!canSend && !sendLocked && !settingsBusy}
        onsettingsbusy={(value) => {
          settingsBusy = value;
        }}
        images={sending.images || []}
        onimages={(images) => sender?.images(images)}
        bind:draft={sending.draft}
        enabled={!!canSend &&
          !sendLocked &&
          !settingsBusy &&
          !permissionState.modePending &&
          !needsApproval}
        locked={sendLocked}
        canStop={selectedLane
          ? selectedAgent?.run_id === thread?.run_id &&
            selectedAgent?.data?.can_interrupt === true &&
            ["running", "waiting"].includes(String(selectedAgent?.data?.status))
          : working || needsApproval}
        stopping={pending?.action === "interrupt" ||
          (busy && (working || needsApproval))}
        onstop={() => void session?.control("interrupt", thread?.run_id)}
        onsend={send}
        onchange={(text) => sender?.draft(text)}
      />{/key}
  </div>

  <MemoryViewer
    selected={selectedMemory}
    available={availableWidth}
    onclose={() => openMemory("")}
  />
  <ThreadTaskViewer
    selected={selectedMemory ? "" : selectedTask}
    available={availableWidth}
    onopen={openTask}
    onclose={() => openTask("")}
  />
</div>

<style>
  .thread-workbench {
    display: flex;
    gap: 24px;
    flex: 1;
    min-width: 0;
    min-height: 0;
    position: relative;
  }
  .lane-hint {
    font-size: 12px;
    color: var(--muted);
    margin: 8px 2px;
  }
  .send-recovery {
    display: flex;
    align-items: center;
    gap: 12px;
    font-size: 12px;
    color: var(--muted);
    padding: 8px 0;
  }
  .send-recovery button {
    width: auto;
    white-space: nowrap;
  }
  .thread-page {
    min-width: 0;
    max-width: 1000px;
    margin: 0 auto;
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }
</style>
