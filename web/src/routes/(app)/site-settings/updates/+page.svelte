<script lang="ts">
  import { useAccount } from "$lib/account-context";
  import { checkPermission } from "$lib/permissions";
  import Updates from "$lib/components/settings/Updates.svelte";
  const current = useAccount();
</script>

{#if checkPermission(current.account, "site.superuser")}<Updates />{:else}<p
    class="notice error"
  >
    Only superusers can manage deployment updates.
  </p>{/if}
