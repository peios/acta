<script lang="ts">
  import { relativeClock, relativeTime } from "$lib/relative-time.js";
  let { value }: { value: string } = $props();
  const full = $derived(
    new Intl.DateTimeFormat(undefined, {
      dateStyle: "full",
      timeStyle: "long",
    }).format(new Date(value)),
  );
</script>

<time datetime={value} title={full} aria-label={full}
  >{relativeTime(value, $relativeClock)}</time
>

<style>
  time {
    white-space: nowrap;
  }
</style>
