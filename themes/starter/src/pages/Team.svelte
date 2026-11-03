<script lang="ts">
  import type { Team } from "@flagfall/theme-sdk";
  import { app, errorText, ff } from "../lib/app.svelte";

  let { params }: { params: Record<string, string> } = $props();

  let team = $state<Team | null>(null);
  let error = $state("");

  $effect(() => {
    app.version.scoreboard;
    ff.teams.get(params.id ?? "").then((t) => (team = t), (err) => (error = errorText(err)));
  });
</script>

{#if error}<p class="error" role="alert">{error}</p>{/if}
{#if team}
  <h1>{team.name}</h1>
  <p class="muted">Rank {team.rank ?? "—"} · {team.score} points · {team.firstBloodCount} first bloods</p>

  <h2>Members</h2>
  <ul>
    {#each team.members as m (m.id)}<li>{m.name}{#if m.role === "captain"} <span class="muted">(captain)</span>{/if} · {m.score} points</li>{/each}
  </ul>

  <h2>Solves</h2>
  <table>
    <thead><tr><th>Challenge</th><th>Category</th><th>Solved by</th><th class="num">Points</th><th>When</th></tr></thead>
    <tbody>
      {#each team.solves as s (s.challengeSlug)}
        <tr>
          <td><a href="/challenges/{s.challengeSlug}">{s.challengeName}</a>{#if s.firstBlood} <span class="muted">(first blood)</span>{/if}</td>
          <td>{s.category}</td>
          <td>{s.userName}</td>
          <td class="num">{s.points}</td>
          <td>{new Date(s.solvedAt).toLocaleString()}</td>
        </tr>
      {:else}
        <tr><td colspan="5" class="muted">No solves yet.</td></tr>
      {/each}
    </tbody>
  </table>
{/if}
