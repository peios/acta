<script lang="ts">
    import { onMount } from "svelte";
    import { mobileViewport } from "$lib/mobile-viewport";
    import { provideAccount } from "$lib/account-context";
    import { api, type Account } from "$lib/api";
    import TaskTextField from "$lib/components/tasks/TaskTextField.svelte";
    import type { Task } from "$lib/tasks";
    import TaskFilterMenu from "$lib/components/tasks/TaskFilterMenu.svelte";
    import { emptyViewFilters } from "$lib/task-views.js";
    import TaskAssigneePicker from "$lib/components/tasks/TaskAssigneePicker.svelte";
    import CommentComposer from "$lib/components/tasks/CommentComposer.svelte";
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
        release_id: "",
        release: null,
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
    onMount(() => {
        void api<Task>("tasks/audit").then((saved) => {
            task = saved;
        });
    });
    let posted = $state(false);
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
</script>

<div class="shell" use:mobileViewport>
    <main>
        <h1>Mobile controls</h1>
        <TaskTextField {task} field="title" editable onsaved={(next) => (task = next)} />
        <TaskTextField {task} field="description" editable onsaved={(next) => (task = next)} />
        <div class="actions">
            <TaskFilterMenu
                workspace="audit"
                {config}
                filters={emptyViewFilters()}
            /><TaskAssigneePicker
                workspace="audit"
                assignees={[]}
                disabled={false}
                onchange={async () => true}
            />
        </div>
        <CommentComposer task="audit" onposted={() => (posted = true)} />
        {#if posted}<p>Comment posted</p>{/if}
    </main>
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
</style>
