<script lang="ts">
  import type { Scoreboard } from "@flagfall/theme-sdk";
  import { app, errorText, ff } from "../lib/app.svelte";

  let { params: _ }: { params: Record<string, string> } = $props();

  let board = $state<Scoreboard | null>(null);
  let bracketId = $state("");
  let error = $state("");

  // Live: the "scoreboard" event bumps the version, so this refetches.
  $effect(() => {
    app.version.scoreboard;
    ff.scoreboard.get(bracketId ? { bracketId } : {}).then((b) => (board = b), (err) => (error = errorText(err)));
  });
</script>

<h1>Scoreboard</h1>
{#if board?.frozen}<p class="card">The scoreboard is frozen; scores show the state at the freeze.</p>{/if}

{#if (app.event?.brackets.length ?? 0) > 1}
  <label>
    Bracket
    <select bind:value={bracketId}>
      <option value="">All teams</option>
      {#each app.event?.brackets ?? [] as b (b.id)}<option value={b.id}>{b.name}</option>{/each}
    </select>
  </label>
{/if}

{#if error}<p class="error" role="alert">{error}</p>{/if}

{#if board}
  <table>
    <caption class="muted">Updated {new Date(board.updatedAt).toLocaleTimeString()}</caption>
    <thead><tr><th class="num">Rank</th><th>Team</th><th class="num">Solves</th><th class="num">Score</th></tr></thead>
    <tbody>
      {#each board.entries as e (e.team.id)}
        <tr class:mine={e.team.id === app.me?.team?.id}>
          <td class="num">{e.rank}</td>
          <td><a href="/teams/{e.team.id}">{e.team.name}</a></td>
          <td class="num">{e.solveCount}</td>
          <td class="num">{e.score}</td>
        </tr>
      {:else}
        <tr><td colspan="4" class="muted">No scores yet.</td></tr>
      {/each}
    </tbody>
  </table>
{/if}

<style>
  .mine td { font-weight: 700; }
  caption { text-align: left; padding-bottom: 0.5rem; }
</style>
