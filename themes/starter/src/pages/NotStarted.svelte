<script lang="ts">
  import { formatDuration } from "@flagfall/theme-sdk";
  import { app } from "../lib/app.svelte";

  let { params: _ }: { params: Record<string, string> } = $props();

  let now = $state(Date.now());
  $effect(() => {
    const id = setInterval(() => (now = Date.now()), 1000);
    return () => clearInterval(id);
  });
  const left = $derived(app.event ? Math.max(0, new Date(app.event.startsAt).getTime() - now) : 0);
</script>

<h1>{app.event?.name ?? "The event"} hasn't started yet</h1>
{#if app.event}
  <p>It starts <time datetime={app.event.startsAt}>{new Date(app.event.startsAt).toLocaleString()}</time>.</p>
  <p class="countdown" aria-live="off">Starts in {formatDuration(left)}</p>
{/if}
{#if !app.me}<p><a href="/sign-up">Create an account</a> now so you're ready.</p>{/if}

<style>
  .countdown { font-size: 2rem; font-weight: 700; font-variant-numeric: tabular-nums; }
</style>
