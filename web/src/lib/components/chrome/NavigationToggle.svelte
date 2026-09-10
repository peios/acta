<script lang="ts">
  import { useNavigation } from "$lib/navigation-context";
  const navigation = useNavigation();
</script>

<button type="button" aria-label="Open navigation" onclick={navigation.open}>
  <svg
    viewBox="0 0 24 24"
    width="20"
    height="20"
    fill="none"
    stroke="currentColor"
    stroke-width="1.6"
    aria-hidden="true"
  >
    <rect x="3" y="4" width="18" height="16" rx="3" /><path d="M9 4v16" />
  </svg>
</button>

<style>
  button {
    display: none;
    width: 44px;
    height: 44px;
    flex-shrink: 0;
    align-items: center;
    justify-content: center;
    padding: 0;
    border: 0;
    background: transparent;
    color: var(--text);
    border-radius: 8px;
    transition:
      background 140ms ease,
      transform 140ms ease;
  }
  button:hover {
    background: var(--hover-surface);
  }
  button:active {
    transform: scale(0.92);
  }
  @media (prefers-reduced-motion: reduce) {
    button {
      transition: none;
    }
    button:active {
      transform: none;
    }
  }
  @media (max-width: 720px) {
    button {
      display: inline-flex;
    }
  }
</style>
