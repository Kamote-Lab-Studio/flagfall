<script lang="ts">
  import { onMount } from "svelte";
  import { api, message } from "../lib/api";
  import { href } from "../lib/router.svelte";
  import ServicesPanel from "../components/ServicesPanel.svelte";
  import type { Dashboard, Event } from "../lib/types";
  import { ago, since } from "../lib/util";

  // A challenge with this many solves or fewer is flagged in red.
  const LOW = 5;
  // At most this many low-solve challenges are listed under "Needs attention".
  const SHOWN = 5;

  let d = $state<Dashboard | null>(null);
  let ev = $state<Event | null>(null);
  let error = $state("");

  async function load() {
    try {
      [d, ev] = await Promise.all([api.get<Dashboard>("/dashboard"), api.get<Event>("/event")]);
      error = "";
    } catch (err) {
      error = message(err);
    }
  }

  // Refresh every 15 seconds, or every 3 while a container is starting.
  onMount(() => {
    let t: ReturnType<typeof setTimeout>;
    let stopped = false;
    const tick = async () => {
      await load();
      if (stopped) return;
      const starting = d?.services.some((s) => s.desired === "running" && s.state === "starting");
      t = setTimeout(tick, starting ? 3_000 : 15_000);
    };
    tick();
    return () => {
      stopped = true;
      clearTimeout(t);
    };
  });

  const visible = $derived((d?.challenges ?? []).filter((c) => c.state === "visible"));
  const bySolves = $derived([...visible].sort((a, b) => b.solves - a.solves));
  const most = $derived(Math.max(1, ...visible.map((c) => c.solves)));
  const started = $derived(ev?.startsAt && new Date(ev.startsAt).getTime() < Date.now() ? ev.startsAt : null);
  const hoursIn = $derived(started ? Math.floor((Date.now() - new Date(started).getTime()) / 3_600_000) : 0);
  // Low-solve challenges only count once the event has run a while.
  const struggling = $derived(hoursIn >= 2 ? bySolves.filter((c) => c.solves <= LOW).reverse() : []);
  const bloods = $derived(
    (d?.challenges ?? [])
      .filter((c) => c.firstBloodTeam && c.firstBloodAt)
      .sort((a, b) => new Date(a.firstBloodAt!).getTime() - new Date(b.firstBloodAt!).getTime()),
  );
</script>

<h1>Dashboard</h1>
{#if error}<p class="notice bad">{error}</p>{/if}
{#if d}
  <div class="tiles">
    <div class="tile"><span class="muted small">Teams</span><b class="tile-num">{d.teams}</b><span class="muted small">{d.teamsWithSolves} with at least one solve</span></div>
    <div class="tile"><span class="muted small">Players</span><b class="tile-num">{d.players}</b><span class="muted small">{d.playersOnline} online now</span></div>
    <div class="tile"><span class="muted small">Solves</span><b class="tile-num accent">{d.solves}</b><span class="muted small">correct flags so far</span></div>
    <div class="tile"><span class="muted small">Last hour</span><b class="tile-num">{d.solvesLastHour}</b><span class="muted small">solves in the past 60 minutes</span></div>
  </div>

  <div class="row">
    <section class="panel pad grow3 stack" aria-labelledby="spc">
      <div class="panel-head"><h2 id="spc">Solves per challenge</h2><span class="muted small">Red: {LOW} solves or fewer</span></div>
      {#if bySolves.length === 0}
        <p class="empty">No visible challenges yet. <a href={href("/challenges")}>Add one</a>.</p>
      {:else}
        <div class="bars">
          {#each bySolves as c (c.id)}
            <div class="bar-row">
              <a class="ellip" href={href(`/challenges/${c.id}`)}>{c.name}</a>
              <span class="bar-track" aria-hidden="true"><span class:low={c.solves <= LOW} style="width: {Math.max(c.solves ? 1 : 0, Math.round((c.solves / most) * 100))}%"></span></span>
              <span class="mono num">{c.solves}</span>
            </div>
          {/each}
        </div>
      {/if}
    </section>

    <section class="panel pad grow2 stack" aria-labelledby="att">
      <h2 id="att">Needs attention</h2>
      {#if d.warnings.length === 0 && struggling.length === 0}
        <p class="empty">Nothing right now.</p>
      {:else}
        <ul class="alerts">
          {#each d.warnings as w (w)}
            <li><span class="dot accent-dot" aria-hidden="true"></span><span>{w}</span></li>
          {/each}
          {#each struggling.slice(0, SHOWN) as c (c.id)}
            <li><span class="dot" aria-hidden="true"></span>
              <span><b>{c.name}</b> has {c.solves} {c.solves === 1 ? "solve" : "solves"} after {hoursIn} hours. <a href={href(`/challenges/${c.id}`)}>Check the challenge</a> or consider a hint.</span></li>
          {/each}
          {#if struggling.length > SHOWN}
            <li><span class="dot" aria-hidden="true"></span>
              <span>{struggling.length - SHOWN} more {struggling.length - SHOWN === 1 ? "challenge has" : "challenges have"} {LOW} solves or fewer. <a href={href("/challenges")}>See all challenges</a></span></li>
          {/if}
        </ul>
      {/if}
    </section>
  </div>

  <div class="row">
    <section class="panel pad grow2 stack" aria-labelledby="ls">
      <div class="panel-head"><h2 id="ls">Latest solves</h2><a class="small" href={href("/submissions")}>All submissions</a></div>
      {#if d.latestSolves.length === 0}
        <p class="empty">No solves yet.</p>
      {:else}
        <ol class="feed">
          {#each d.latestSolves as s (s.teamId + s.challengeId)}
            <li><span class="mono muted">{ago(s.solvedAt)}</span>
              <span><a href={href(`/teams/${s.teamId}`)}><b>{s.teamName}</b></a> solved {s.challengeName}</span>
              <span class="pts mono">+{s.points}</span></li>
          {/each}
        </ol>
      {/if}
    </section>

    <section class="panel grow2 table-panel" aria-labelledby="fb">
      <h2 id="fb">First bloods</h2>
      {#if bloods.length === 0}
        <p class="empty" style="padding: 0 24px 14px">Nobody has solved anything yet.</p>
      {:else}
        <table>
          <thead><tr><th scope="col">Challenge</th><th scope="col">Team</th><th scope="col" class="num">Time</th></tr></thead>
          <tbody>
            {#each bloods as c (c.id)}
              <tr><td>{c.name}</td><td><b>{c.firstBloodTeam}</b></td>
                <td class="num mono muted">{started ? since(started, c.firstBloodAt!) : "—"}</td></tr>
            {/each}
          </tbody>
        </table>
      {/if}
    </section>
  </div>

  {#if d.services.length}
    <ServicesPanel services={d.services} enabled={d.servicesEnabled} onchange={load} />
  {/if}
  <p class="hint">Refreshes every 15 seconds.</p>
{:else if !error}
  <p class="muted">Loading…</p>
{/if}
