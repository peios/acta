<script lang="ts">
  import NavigationToggle from "$lib/components/chrome/NavigationToggle.svelte";
  import { useThreads } from "$lib/threads.svelte";
  const threads = useThreads();
</script>

<NavigationToggle />
<div class="thread-empty">
  <svg
    width="32"
    height="32"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width="1.4"
    aria-hidden="true"
    ><rect x="3" y="5" width="18" height="15" rx="4" /><path
      d="M12 2v3M8 11v2m8-2v2m-7 4h6"
    /></svg
  >
  <h2>Your agents, in one place</h2>
  <p>Open a thread from the sidebar, or create one on a connected harness.</p>
  <button class="primary" onclick={() => (threads.createOpen = true)}
    >New thread</button
  >
</div>

<style>
  .thread-empty {
    max-width: 520px;
    margin: 15vh auto;
    text-align: center;
    color: var(--muted);
  }
  h2 {
    color: var(--text);
    font-size: 20px;
    font-weight: 550;
  }
  p {
    font-size: 14px;
    line-height: 1.7;
  }
  button {
    width: auto;
    margin-top: 16px;
  }
</style>
