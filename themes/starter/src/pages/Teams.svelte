<script lang="ts">
  import type { TeamSummary } from "@flagfall/theme-sdk";
  import { errorText, ff } from "../lib/app.svelte";

  let { params: _ }: { params: Record<string, string> } = $props();

  let search = $state("");
  let page = $state(1);
  let teams = $state<TeamSummary[]>([]);
  let total = $state(0);
  let error = $state("");
  const pageSize = 50;

  $effect(() => {
    const query = { search: search.trim() || undefined, page, pageSize };
    ff.teams.list(query).then(
      (r) => {
        teams = r.items;
        total = r.total;
      },
      (err) => (error = errorText(err)),
    );
  });
</script>

<h1>Teams</h1>
<label>Search teams <input type="search" bind:value={search} oninput={() => (page = 1)} /></label>
{#if error}<p class="error" role="alert">{error}</p>{/if}

<table>
  <thead><tr><th class="num">Rank</th><th>Team</th><th class="num">Members</th><th class="num">Score</th></tr></thead>
  <tbody>
    {#each teams as t (t.id)}
      <tr>
        <td class="num">{t.rank ?? "—"}</td>
        <td><a href="/teams/{t.id}">{t.name}</a>{#if t.recruiting} <span class="muted">(recruiting)</span>{/if}</td>
        <td class="num">{t.memberCount}</td>
        <td class="num">{t.score}</td>
      </tr>
    {:else}
      <tr><td colspan="4" class="muted">No teams found.</td></tr>
    {/each}
  </tbody>
</table>

{#if total > pageSize}
  <p class="pager">
    <button type="button" class="secondary" disabled={page === 1} onclick={() => page--}>Previous</button>
    <span>Page {page} of {Math.ceil(total / pageSize)}</span>
    <button type="button" class="secondary" disabled={page * pageSize >= total} onclick={() => page++}>Next</button>
  </p>
{/if}

<style>
  label { margin-bottom: 1rem; max-width: 20rem; font-weight: 400; }
  .pager { display: flex; gap: 1rem; align-items: center; margin-top: 1rem; }
</style>
