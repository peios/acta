<script lang="ts">
  import ThreadToolImages from "./ThreadToolImages.svelte";
  import { loadImage } from "$lib/thread-image-input.js";
  import type { UserMessage } from "$lib/thread-feed.js";
  let { message }: { message: UserMessage } = $props();
  let previews = $state<unknown[]>([]);
  $effect(() => {
    const refs = message.draftImages || [];
    let active = true;
    void Promise.all(refs.map(loadImage))
      .then((images) => {
        if (active) previews = images;
      })
      .catch(() => {});
    return () => {
      active = false;
    };
  });
</script>

<article
  class="user-message"
  class:completed={message.completed}
  aria-label={message.completed
    ? "User message, completed"
    : "User message, in progress"}
>
  {#if message.images?.length || previews.length}<ThreadToolImages
      images={message.images || previews}
      label="Attached image"
    />{/if}
  {#if message.text}<span>{message.text}</span>{/if}
  {#if message.sending}<small role="status"
      >{message.sending === "rejected"
        ? "Not sent"
        : message.sending === "uncertain"
          ? "Delivery unconfirmed"
          : "Sending…"}</small
    >{/if}
</article>

<style>
  .user-message {
    margin-inline-start: auto;
    width: fit-content;
    max-width: 85%;
    min-width: 0;
    padding: 12px 16px;
    border-radius: 16px 16px 4px 16px;
    background: color-mix(in srgb, var(--text) 8%, var(--surface));
    color: var(--text);
    font-size: 14px;
    line-height: 1.65;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    transition: background-color 220ms ease;
  }
  small {
    display: block;
    color: var(--muted);
    font-size: 11px;
    margin-top: 4px;
  }
  .completed {
    background: color-mix(in srgb, #6b94d5 22%, var(--surface));
  }
  @media (max-width: 600px) {
    .user-message {
      max-width: 92%;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .user-message {
      transition: none;
    }
  }
</style>
