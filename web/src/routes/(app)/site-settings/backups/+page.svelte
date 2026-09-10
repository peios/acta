<script lang="ts">
  import { useAccount } from "$lib/account-context";
  import { checkPermission } from "$lib/permissions";
  import Backups from "$lib/components/settings/Backups.svelte";
  const current = useAccount();
</script>

{#if checkPermission(current.account, "site.backups.manage")}<Backups
  />{:else}<p class="notice error">
    You don’t have permission to manage backups.
  </p>{/if}
