<script lang="ts">
  import type { Snippet } from "svelte";
  let {
    title,
    description,
    eyebrow,
    children,
  }: {
    title: string;
    description: string;
    eyebrow?: string;
    children: Snippet;
  } = $props();
</script>

<main class="auth-page">
  <div class="auth-wrap">
    <div class="wordmark" aria-label="Acta">
      acta<span aria-hidden="true">.</span>
    </div>
    <section class="auth-panel" aria-labelledby="page-title">
      {#if eyebrow}<p class="eyebrow">{eyebrow}</p>{/if}
      <h1 id="page-title">{title}</h1>
      <p class="intro">{description}</p>
      {@render children()}
    </section>
    <p class="auth-footer">A place for work to come together.</p>
  </div>
</main>
