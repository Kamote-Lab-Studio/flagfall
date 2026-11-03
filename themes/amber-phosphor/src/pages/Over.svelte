<script lang="ts">
  import type { Scoreboard } from "@flagfall/theme-sdk";
  import { app, ff } from "../lib/state.svelte";
  import { fmt } from "../lib/util";

  let board = $state<Scoreboard | null>(null);

  $effect(() => {
    void app.scoreVersion;
    ff.scoreboard.get().then((b) => (board = b));
  });

  const mine = $derived(board?.entries.find((e) => e.team.id === app.me?.team?.id));
</script>

<main class="state-screen">
  <h1 class="term-big">That's a wrap.</h1>
  <p class="muted">{app.event?.name ?? "The event"} is over. Thanks for playing.{#if board?.frozen} Final results are revealed soon.{/if}</p>

  {#if board && board.entries.length}
    <ol class="podium-list" aria-label="Top teams">
      {#each board.entries.slice(0, 3) as e (e.team.id)}
        <li><span class="place">{e.rank}</span><a href="/teams/{e.team.id}">{e.team.name}</a><span class="pts">{fmt(e.score)}</span></li>
      {/each}
    </ol>
  {/if}
  {#if mine && mine.rank > 3}
    <p>Your team finished <b class="amber">#{mine.rank}</b> with {fmt(mine.score)} points.</p>
  {/if}
  <div class="row-actions">
    <a class="btn" href="/scoreboard">Full scoreboard</a>
    <a class="btn-ghost" href="/challenges">Browse the challenges</a>
  </div>
</main>
