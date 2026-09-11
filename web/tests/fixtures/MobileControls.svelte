<script lang="ts">
  import { onMount } from "svelte";
  import { mobileViewport } from "$lib/mobile-viewport";
  import { provideAccount } from "$lib/account-context";
  import { api, type Account } from "$lib/api";
  import type { ApprovalItem } from "$lib/thread-permissions.js";
  import TaskTextField from "$lib/components/tasks/TaskTextField.svelte";
  import type { Task } from "$lib/tasks";
  import TaskFilterMenu from "$lib/components/tasks/TaskFilterMenu.svelte";
  import TaskAssigneePicker from "$lib/components/tasks/TaskAssigneePicker.svelte";
  import CommentComposer from "$lib/components/tasks/CommentComposer.svelte";
  import ThreadComposer from "$lib/components/threads/ThreadComposer.svelte";
  provideAccount({
    account: { id: "mobile-audit" } as Account,
    update: () => {},
  });
  let task = $state<Task>({
    id: "audit",
    workspace_id: "audit",
    number: 1,
    archived: false,
    archived_at: null,
    priority: "",
    type: "",
    size: "",
    status_id: "todo",
    parent_id: "",
    assignees: [],
    descendant_assignees: [],
    ancestors: [],
    children: 0,
    created_at: "2026-09-11T00:00:00Z",
    updated_at: "2026-09-11T00:00:00Z",
    reference: "QA-1",
    title: "Mobile task",
    description: "Initial description",
    versions: { title: 1, description: 1 },
  });
  let draft = $state("");
  onMount(() => {
    void api<Task>("tasks/audit").then((saved) => {
      task = saved;
    });
  });
  let connected = $state(true);
  let approvals = $state<ApprovalItem[]>([]);
  let answer = $state("");
  let posted = $state(false);
  let mode = $state("ask");
  const permissions = {
    results: {},
    busy: {},
    errors: {},
    checked: {},
    modePending: "",
    modeError: "",
  };
  const config = {
    prefix: "QA",
    previous_prefixes: [],
    statuses: [
      { id: "todo", name: "To do" },
      { id: "done", name: "Done" },
    ],
    creation_status: "todo",
    completed_status: "done",
    version: 1,
    revision: 1,
  };
  function request(question: boolean) {
    const id = crypto.randomUUID();
    approvals = [
      {
        id,
        frame: {
          kind: question ? "question/request" : "approval/request",
        } as ApprovalItem["frame"],
        data: {
          approval_id: id,
          title: "Run a command in the development workspace",
          reason: "Check the application before the mobile release.",
          details: {
            command:
              "printf '%s' 'A long command with enough detail to wrap within a narrow viewport'",
            cwd: "/home/reviewer/projects/acta",
          },
          ...(question
            ? {
                questions: [
                  {
                    id: "q",
                    header: "Approach",
                    text: "Which approach should I use?",
                    multiple: false,
                    options: [
                      {
                        label: "First",
                        description: "Keep the current implementation.",
                      },
                      {
                        label: "Second",
                        description: "Use the alternative implementation.",
                      },
                    ],
                  },
                ],
              }
            : {}),
        },
        status: "pending",
        error: "",
        busy: false,
        available: connected,
      },
    ];
  }
</script>

<div class="shell" use:mobileViewport>
  <main>
    <h1>Mobile controls</h1>
    <TaskTextField
      {task}
      field="title"
      editable
      onsaved={(next) => (task = next)}
    />
    <TaskTextField
      {task}
      field="description"
      editable
      onsaved={(next) => (task = next)}
    />
    <div class="actions">
      <TaskFilterMenu workspace="audit" {config} /><TaskAssigneePicker
        workspace="audit"
        assignees={[]}
        disabled={false}
        onchange={async () => true}
      />
    </div>
    <CommentComposer task="audit" onposted={() => (posted = true)} />
    {#if posted}<p>Comment posted</p>{/if}
    <div class="actions">
      <button onclick={() => request(false)}>Request approval</button><button
        onclick={() => request(true)}>Ask question</button
      ><button
        onclick={() => {
          connected = !connected;
          approvals = approvals.map((a) => ({ ...a, available: connected }));
        }}>Toggle connection</button
      >
    </div>
    <output>{answer}</output>
  </main>
  <footer>
    <ThreadComposer
      permissionState={permissions}
      {approvals}
      answerApproval={(_item, decision, values) => {
        answer = decision + JSON.stringify(values ?? {});
        approvals = [];
      }}
      editQuestion={(item, id, value) => {
        approvals = approvals.map((a) =>
          a.id === item.id ? { ...a, draft: { ...a.draft, [id]: value } } : a,
        );
      }}
      changePermission={(value) => (mode = value)}
      configuration={{ model: "small", effort: "low", permissions: { mode } }}
      configurationSequence={1}
      threadId="audit"
      runId="run"
      available={connected}
      settingsEditable={connected}
      permissionsEditable={connected}
      onsettingsbusy={() => {}}
      bind:draft
      enabled={connected}
      canStop={connected}
      onstop={() => {}}
      onsend={() => (draft = "")}
      onchange={() => {}}
    />
  </footer>
</div>

<style>
  .shell {
    position: fixed;
    top: var(--mobile-viewport-top, 0px);
    inset-inline: 0;
    height: var(--mobile-viewport-height, 100dvh);
    display: flex;
    flex-direction: column;
  }
  main {
    flex: 1;
    overflow: auto;
    min-height: 0;
    padding: 16px;
  }
  h1 {
    font-size: 20px;
  }
  .actions {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
    margin: 12px 0;
  }
  footer {
    padding: 12px;
  }
</style>
