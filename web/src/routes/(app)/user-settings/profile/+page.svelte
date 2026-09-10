<script lang="ts">
  import { useAccount } from "$lib/account-context";
  import ProfileForm from "$lib/components/settings/ProfileForm.svelte";
  const session = useAccount();
</script>

<ProfileForm account={session.account} onSaved={session.update} />
