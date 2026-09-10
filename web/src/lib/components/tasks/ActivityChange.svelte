<script lang="ts">
  import {
    activityAction,
    activityValue,
    type ActivityEntry,
  } from "$lib/activity";
  import RelativeTime from "../RelativeTime.svelte";
  let { entry }: { entry: ActivityEntry } = $props();
  const name = $derived(entry.actor.display_name || entry.actor.username);
  const changed = $derived(
    entry.kind === "task.changed" && entry.field !== "description",
  );
  const detail = $derived(
    `${name}${entry.actor.owner_id ? " (Agent)" : ""} ${activityAction(entry)}${changed ? `: ${activityValue(entry.before)} → ${activityValue(entry.after)}` : ""}${entry.reason === "status_replaced" ? " · Workspace status replacement" : ""}${entry.count > 1 ? ` · ${entry.count} changes` : ""}`,
  );
</script>

<div class="event">
  <span
    class="avatar"
    class:agent={!!entry.actor.owner_id}
    title={`@${entry.actor.username}${entry.actor.owner_id ? " · Agent" : ""}`}
    aria-hidden="true">{name.slice(0, 1).toUpperCase()}</span
  >
  <p title={detail}>
    <strong>{name}</strong>{#if entry.actor.owner_id}<span class="agent-label"
        >Agent</span
      >{/if}
    {activityAction(entry)}{#if changed}<span class="change">
        · {activityValue(entry.before)} →
        <span>{activityValue(entry.after)}</span></span
      >{/if}{#if entry.reason === "status_replaced"}<span>
        · Workspace status replacement</span
      >{/if}{#if entry.count > 1}<span class="count">
        · {entry.count} changes</span
      >{/if}
  </p>
  <span class="time"><RelativeTime value={entry.updated_at} /></span>
</div>

<style>
  .event {
    display: flex;
    align-items: center;
    gap: 9px;
    min-width: 0;
    padding: 9px 10px;
  }
  .avatar {
    display: grid;
    place-items: center;
    flex: 0 0 22px;
    height: 22px;
    border-radius: 50%;
    background: var(--scope-active);
    color: var(--accent);
    font-size: 10px;
    font-weight: 600;
  }
  .avatar.agent {
    border-radius: 7px;
  }
  p {
    flex: 1;
    min-width: 0;
    margin: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: 12px;
    line-height: 1.7;
    color: var(--muted);
  }
  strong {
    font-weight: 550;
    color: var(--text);
  }
  .change span {
    color: var(--text);
  }
  .agent-label {
    font-size: 9px;
    border: 1px solid var(--panel-border);
    border-radius: 4px;
    padding: 1px 4px;
    margin-left: 5px;
  }
  .count {
    font-size: 10px;
  }
  .time {
    flex: none;
    color: var(--muted);
    font-size: 10px;
  }
  @media (max-width: 420px) {
    .event {
      padding: 8px 5px;
      gap: 7px;
    }
    .agent-label {
      display: none;
    }
  }
</style>
