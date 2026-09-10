<script lang="ts">
  import AuthShell from "$lib/components/AuthShell.svelte";
</script>

<svelte:head
  ><title>Account disabled · Acta</title><meta
    name="robots"
    content="noindex"
  /></svelte:head
>
<AuthShell title="Account disabled" description="Contact your administrator."
  ><p>
    Your account currently has no access to this Acta installation. Your
    administrator can help restore access.
  </p>
  <a href="/login">Back to sign in</a></AuthShell
>

<style>
  p {
    font-size: 14px;
    line-height: 1.7;
    color: var(--muted);
  }
  a {
    font-size: 13px;
    color: var(--accent);
  }
</style>
