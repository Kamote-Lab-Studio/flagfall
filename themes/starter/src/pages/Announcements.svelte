<script lang="ts">
  import type { Announcement } from "@flagfall/theme-sdk";
  import { app, errorText, ff } from "../lib/app.svelte";

  let { params: _ }: { params: Record<string, string> } = $props();

  let list = $state<Announcement[]>([]);
  let error = $state("");

  $effect(() => {
    app.version.announcements;
    ff.announcements.list().then((a) => (list = a), (err) => (error = errorText(err)));
  });
</script>

<h1>Announcements</h1>
{#if error}<p class="error" role="alert">{error}</p>{/if}
{#each list as a (a.id)}
  <article class="card">
    <h2>{a.title}{#if a.pinned} <span class="muted">(pinned)</span>{/if}</h2>
    <p class="muted"><time datetime={a.postedAt}>{new Date(a.postedAt).toLocaleString()}</time></p>
    <div>{@html a.bodyHtml}</div>
  </article>
{:else}
  <p class="muted">No announcements yet.</p>
{/each}

<style>
  h2 { margin-top: 0; }
</style>
