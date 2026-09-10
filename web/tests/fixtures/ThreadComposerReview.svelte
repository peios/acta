<script lang="ts">
  import ThreadComposer from "$lib/components/threads/ThreadComposer.svelte";
  import { provideAccount } from "$lib/account-context";
  import type { Account } from "$lib/api";
  provideAccount({ account: { id: "review" } as Account, update: () => {} });
  let draft = $state("");
  let stopped = $state(false);
  let sent = $state<string[]>([]);
  let selectedMode = $state("ask");
  let connected = $state(true);
  const permissions = {
    results: {},
    busy: {},
    errors: {},
    checked: {},
    modePending: "",
    modeError: "",
  };
</script>

<ThreadComposer
  permissionState={permissions}
  approvals={[]}
  answerApproval={() => {}}
  changePermission={(mode) => (selectedMode = mode)}
  configuration={{ model: "Review model", permissions: { mode: selectedMode } }}
  configurationSequence={1}
  threadId="review"
  runId="run"
  available={true}
  settingsEditable={false}
  permissionsEditable={connected}
  onsettingsbusy={() => {}}
  bind:draft
  enabled={true}
  canStop={!stopped}
  onstop={() => (stopped = true)}
  onsend={() => {
    sent = [...sent, draft];
    draft = "";
  }}
  onchange={() => {}}
/>
<output>{sent.join("|")}</output>

<button onclick={() => (connected = !connected)}>Toggle connection</button>
