<script lang="ts">
  import WorkspaceSwitcher from "$lib/components/workspaces/WorkspaceSwitcher.svelte";
  import { provideAccount } from "$lib/account-context";
  import { provideWorkspace } from "$lib/workspaces";
  provideAccount({
    account: {
      id: "user",
      username: "jack",
      display_name: null,
      permissions: ["site.workspaces.create"],
      direct_permissions: [],
      require_mfa: false,
      direct_require_mfa: false,
      groups: [],
      can_create_users: false,
      mfa_setup_required: false,
      permissions_version: 1,
      last_active_superuser: false,
      profile_version: 1,
      previous_usernames: [],
    },
    update: () => {},
  });
  provideWorkspace({
    workspace: {
      id: "peios",
      name: "Peios",
      slug: "peios",
      description: "",
      version: 1,
      permissions: [],
      previous_slugs: [],
    },
    update: () => {},
  });
  let clicks = $state(0);
</script>

<div style="padding:16px;width:240px"><WorkspaceSwitcher /></div>
<button
  style="position:absolute;bottom:80px;right:20px"
  onclick={() => clicks++}>Underlying task</button
>
<output>{clicks}</output>
