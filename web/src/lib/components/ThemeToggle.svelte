<script lang="ts">
  import { onMount } from "svelte";
  let { placement = "corner" }: { placement?: "corner" | "inline" } = $props();
  const maskId = $props.id();
  let dark = $state(false);
  let ready = $state(false);
  onMount(() => {
    const sync = () => {
      dark = document.documentElement.dataset.theme === "dark";
    };
    sync();
    ready = true;
    window.addEventListener("acta:theme-change", sync);
    return () => window.removeEventListener("acta:theme-change", sync);
  });
</script>

<button
  class="theme-toggle"
  class:corner={placement === "corner"}
  class:inline={placement === "inline"}
  class:dark
  class:ready
  type="button"
  aria-label={dark ? "Switch to light mode" : "Switch to dark mode"}
  title={dark ? "Switch to light mode" : "Switch to dark mode"}
  onclick={() => window.dispatchEvent(new Event("acta:toggle-theme"))}
>
  <svg
    viewBox="0 0 24 24"
    width="22"
    height="22"
    fill="none"
    aria-hidden="true"
  >
    <defs>
      <mask id={maskId}>
        <rect width="24" height="24" fill="white" />
        <circle class="cutout" cx="18" cy="6" r="6" fill="black" />
      </mask>
    </defs>
    <g mask={`url(#${maskId})`}>
      <circle class="disc" cx="12" cy="12" r="5" fill="currentColor" />
    </g>
    <g
      class="rays"
      stroke="currentColor"
      stroke-width="1.7"
      stroke-linecap="round"
    >
      <path
        d="M12 2v2m0 16v2M2 12h2m16 0h2M4.93 4.93l1.42 1.42m11.3 11.3 1.42 1.42M4.93 19.07l1.42-1.42m11.3-11.3 1.42-1.42"
      />
    </g>
  </svg>
</button>

<style>
  .theme-toggle {
    width: 44px;
    height: 44px;
    display: grid;
    place-items: center;
    color: var(--text);
    background: var(--surface);
    border: 1px solid var(--panel-border);
    border-radius: 50%;
    box-shadow: var(--shadow);
    visibility: hidden;
    transition:
      transform 180ms ease,
      background-color 180ms ease,
      border-color 180ms ease;
  }
  .corner {
    position: absolute;
    z-index: 10;
    top: max(20px, env(safe-area-inset-top));
    right: max(20px, env(safe-area-inset-right));
  }
  .inline {
    flex-shrink: 0;
    width: 38px;
    background: transparent;
    border-color: transparent;
    box-shadow: none;
  }
  .ready {
    visibility: visible;
  }
  .theme-toggle:hover {
    transform: translateY(-2px);
    background: var(--hover-surface);
  }
  .theme-toggle:active {
    transform: scale(0.92);
  }
  .disc,
  .rays {
    transform-origin: 12px 12px;
  }
  .disc {
    transition: transform 420ms cubic-bezier(0.22, 1, 0.36, 1);
  }
  .rays {
    transition:
      transform 420ms cubic-bezier(0.22, 1, 0.36, 1),
      opacity 180ms ease;
  }
  .cutout {
    transform: translate(9px, -9px);
    transition: transform 420ms cubic-bezier(0.22, 1, 0.36, 1);
  }
  .dark .disc {
    transform: scale(1.6);
  }
  .dark .rays {
    transform: rotate(90deg) scale(0.5);
    opacity: 0;
  }
  .dark .cutout {
    transform: translate(0, 0);
  }
  @media (prefers-reduced-motion: reduce) {
    .theme-toggle,
    .disc,
    .rays,
    .cutout {
      transition: none;
    }
    .theme-toggle:hover,
    .theme-toggle:active {
      transform: none;
    }
  }
</style>
