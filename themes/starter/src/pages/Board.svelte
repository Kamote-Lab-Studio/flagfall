<script lang="ts">
  import type { ChallengeSummary } from "@flagfall/theme-sdk";
  import { app, errorText, ff, setting } from "../lib/app.svelte";

  let { params: _ }: { params: Record<string, string> } = $props();

  let list = $state<ChallengeSummary[]>([]);
  let error = $state("");
  let category = $state("");

  // Fetch again whenever a live event says challenges changed.
  $effect(() => {
    app.version.challenges;
    ff.challenges.list().then((c) => (list = c), (err) => (error = errorText(err)));
  });

  const shown = $derived(category ? list.filter((c) => c.category === category) : list);
</script>

<h1>Challenges</h1>
<p class="muted">{setting("tagline")}</p>

{#if app.me && !app.me.team}
  <p class="card">You're not on a team yet, so you can't submit flags. <a href="/account">Create or join a team</a>.</p>
{/if}

<label class="filter">
  Category
  <select bind:value={category}>
    <option value="">All</option>
    {#each app.event?.categories ?? [] as c (c)}<option value={c}>{c}</option>{/each}
  </select>
</label>

{#if error && !app.me}
  <!-- Events can keep challenges private until players sign in. -->
  <p><a href="/sign-in">Sign in</a> to see the challenges.</p>
{:else if error}
  <p class="error" role="alert">{error}</p>
{/if}

<ul class="tiles">
  {#each shown as c (c.slug)}
    <li>
      <a href="/challenges/{c.slug}" class:solved={c.solved}>
        <span class="muted">{c.category} · {c.difficulty}</span>
        <b>{c.name}</b>
        <span>{c.value} points · {c.solveCount} {c.solveCount === 1 ? "solve" : "solves"}</span>
        {#if c.solved}<span class="ok"> · Solved</span>{/if}
      </a>
    </li>
  {:else}
    <li class="muted">No challenges yet.</li>
  {/each}
</ul>

<style>
  .filter { display: inline-grid; margin: 0.5rem 0 1rem; font-weight: 400; }
</style>
