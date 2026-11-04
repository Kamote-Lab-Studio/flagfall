<script lang="ts">
  import { relativeTime, type Scoreboard, type ScoreSeries } from "@flagfall/theme-sdk";
  import { app, ff, serverNow, t } from "../lib/state.svelte";
  import { fmt } from "../lib/util";
  import Chart from "../components/Chart.svelte";

  let bracketId = $state("");
  let board = $state<Scoreboard | null>(null);
  let series = $state<ScoreSeries[]>([]);
  let shown = $state(100);

  const myTeamId = $derived(app.me?.team?.id);

  async function load() {
    const q = bracketId ? { bracketId } : {};
    const [b, s] = await Promise.all([ff.scoreboard.get(q), ff.scoreboard.series({ ...q, top: 3, teamId: myTeamId })]);
    board = b;
    series = s;
  }

  $effect(() => {
    void app.scoreVersion;
    void bracketId;
    load();
  });

  const bracketName = (id: string) => app.event?.brackets.find((b) => b.id === id)?.name ?? "";
  const start = $derived(app.event?.startsAt ? new Date(app.event.startsAt).getTime() : Date.now() - 3_600_000);
  const end = $derived(Math.min(serverNow(), app.event?.endsAt ? new Date(app.event.endsAt).getTime() : Infinity));
  const freezeNote = $derived.by(() => {
    const ev = app.event;
    if (!ev?.freezeAt || !ev.endsAt) return "";
    const h = Math.round((new Date(ev.endsAt).getTime() - new Date(ev.freezeAt).getTime()) / 3_600_000);
    return `The scoreboard freezes ${h} hour${h === 1 ? "" : "s"} before the end.`;
  });
</script>

<main class="narrow">
  <div class="page-head">
    <div>
      <h1>{t("nav.scoreboard")}</h1>
      <p class="muted">{board ? `${fmt(board.entries.length)} teams on the board.` : ""} {freezeNote}</p>
    </div>
    {#if (app.event?.brackets.length ?? 0) > 1}
      <div class="filters" role="group" aria-label="Bracket">
        <button type="button" aria-pressed={bracketId === ""} onclick={() => (bracketId = "")}>All</button>
        {#each app.event?.brackets ?? [] as b}
          <button type="button" aria-pressed={bracketId === b.id} onclick={() => (bracketId = b.id)}>{b.name}</button>
        {/each}
      </div>
    {/if}
  </div>

  {#if board?.frozen}
    <div class="banner"><span>The scoreboard is frozen. Solves after the freeze are hidden until the results are revealed.</span></div>
  {/if}

  {#if series.length}
    <section class="panel" aria-labelledby="chart-title">
      <div class="panel-head"><h2 id="chart-title">Score over time</h2><span class="muted">Top 3{myTeamId && series.some((s) => s.teamId === myTeamId) ? " and your team" : ""}, since the start</span></div>
      <Chart {series} {start} {end} highlight={myTeamId} />
    </section>
  {/if}

  <section aria-labelledby="rank-title">
    <h2 id="rank-title" class="h2">Rankings</h2>
    {#if !board}
      <p class="loading">Loading…</p>
    {:else if board.entries.length === 0}
      <p class="muted">No solves yet. The first team to score shows up here.</p>
    {:else}
      <div class="table-wrap">
        <table>
          <thead><tr><th scope="col">Rank</th><th scope="col">Team</th><th scope="col">Country</th><th scope="col">Bracket</th><th scope="col" class="num">Solves</th><th scope="col" class="num">Points</th><th scope="col" class="num">Last solve</th></tr></thead>
          <tbody>
            {#each board.entries.slice(0, shown) as e (e.team.id)}
              <tr class:me-row={e.team.id === myTeamId}>
                <td class="rank" class:podium={e.rank <= 3}>{e.rank}</td>
                <td><a href="/teams/{e.team.id}">{e.team.name}</a>{#if e.team.id === myTeamId} <span class="you">You</span>{/if}</td>
                <td class="mono muted">{e.team.country ?? ""}</td>
                <td class="muted">{bracketName(e.team.bracketId)}</td>
                <td class="num mono">{e.solveCount}</td>
                <td class="num pts">{fmt(e.score)}</td>
                <td class="num muted">{e.lastSolveAt ? relativeTime(e.lastSolveAt, t, new Date(serverNow())) : ""}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
      {#if board.entries.length > shown}
        <button type="button" class="btn-ghost" onclick={() => (shown += 200)}>Show more ({fmt(board.entries.length - shown)} left)</button>
      {/if}
    {/if}
  </section>
</main>
