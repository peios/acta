<script lang="ts">
  import { untrack, onMount } from "svelte";
  import { page } from "$app/state";
  import { goto } from "$app/navigation";
  import { api, APIError, errorMessage } from "$lib/api";
  import { useWorkspace, workspacePath, type Workspace } from "$lib/workspaces";
  let { children } = $props();
  const current = useWorkspace();
  // Task query-string navigation stays within the same workspace.
  const routeSlug = $derived(page.params.slug);
  let loaded = $state(""),
    error = $state("");
  let generation = 0,
    visited = "";
  async function load(slug: string) {
    const g = ++generation;
    error = "";
    if (loaded !== slug) {
      loaded = "";
      current.update(null);
    }
    try {
      const w = await api<Workspace>(
        `workspace-slugs/${encodeURIComponent(slug)}`,
      );
      if (g !== generation) return;
      current.update(w);
      loaded = w.slug;
      if (w.slug !== slug) {
        const base = "/workspaces/" + encodeURIComponent(slug);
        await goto(
          workspacePath(w) +
            page.url.pathname.slice(base.length) +
            page.url.search +
            page.url.hash,
          {
            replaceState: true,
          },
        );
        return;
      }
      if (visited !== w.id) {
        try {
          await api(`workspaces/${w.id}/visit`, {});
          if (g === generation) visited = w.id;
        } catch (e) {
          if (g === generation) error = errorMessage(e);
        }
      }
    } catch (e) {
      if (g === generation) {
        error = errorMessage(e);
        if (e instanceof APIError && [401, 403, 404].includes(e.status)) {
          loaded = "";
          current.update(null);
        }
      }
    }
  }
  $effect(() => {
    const slug = routeSlug;
    if (slug) untrack(() => void load(slug));
  });
  onMount(() => {
    const refresh = () => {
      if (page.params.slug) void load(page.params.slug);
    };
    window.addEventListener("focus", refresh);
    window.addEventListener("acta:workspaces-changed", refresh);
    window.addEventListener("acta:permissions-changed", refresh);
    return () => {
      generation++;
      window.removeEventListener("focus", refresh);
      window.removeEventListener("acta:workspaces-changed", refresh);
      window.removeEventListener("acta:permissions-changed", refresh);
      current.update(null);
    };
  });
</script>

{#if error}<p class="notice error" role="alert">{error}</p>
  <button class="secondary" onclick={() => void load(routeSlug!)}
    >Try again</button
  >
  {#if !current.workspace}<a href="/workspaces">Return to your workspaces</a
    >{/if}
{/if}
{#if loaded === page.params.slug && current.workspace}{@render children()}{:else if !error}<p
    class="hint"
    role="status"
  >
    Loading workspace…
  </p>{/if}
