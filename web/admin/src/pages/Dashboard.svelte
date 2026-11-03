<script lang="ts">
  import { onMount } from "svelte";
  import { api, message } from "../lib/api";
  import { href } from "../lib/router.svelte";
  import ServicesPanel from "../components/ServicesPanel.svelte";
  import type { Dashboard } from "../lib/types";
  import { when } from "../lib/util";

  let d = $state<Dashboard | null>(null);
  let error = $state("");

  async function load() {
    try {
      d = await api.get<Dashboard>("/dashboard");
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
</script>

<h1>Dashboard</h1>
{#if error}<p class="notice bad">{error}</p>{/if}
{#if d}
  <div class="stats">
    <div class="stat"><span>Event</span><b>{d.state}</b></div>
    <div class="stat"><span>Players online</span><b>{d.playersOnline}</b></div>
    <div class="stat"><span>Players</span><b>{d.players}</b></div>
    <div class="stat"><span>Teams</span><b>{d.teams}</b></div>
    <div class="stat"><span>Solves</span><b>{d.solves}</b></div>
    <div class="stat"><span>Submissions / min</span><b>{d.submissionsPerMinute.toFixed(1)}</b></div>
  </div>

  {#each d.warnings as w (w)}<div class="notice">{w}</div>{/each}

  <h2>Challenges</h2>
  {#if d.challenges.length === 0}
    <p class="empty">No challenges yet. <a href={href("/challenges")}>Add one</a>.</p>
  {:else}
    <table>
      <thead><tr><th>Challenge</th><th>Category</th><th>State</th><th class="num">Solves</th><th class="num">Attempts</th><th>First blood</th></tr></thead>
      <tbody>
        {#each d.challenges as c (c.id)}
          <tr>
            <td><a href={href(`/challenges/${c.id}`)}>{c.name}</a></td>
            <td>{c.category}</td>
            <td><span class="badge" class:ok={c.state === "visible"}>{c.state}</span></td>
            <td class="num">{c.solves}</td>
            <td class="num">{c.attempts}</td>
            <td>{#if c.firstBloodTeam}{c.firstBloodTeam} <span class="muted">{when(c.firstBloodAt)}</span>{:else}<span class="muted">—</span>{/if}</td>
          </tr>
        {/each}
      </tbody>
    </table>
  {/if}
  {#if d.services.length}
    <ServicesPanel services={d.services} enabled={d.servicesEnabled} onchange={load} />
  {/if}
  <p class="hint">Refreshes every 15 seconds.</p>
{:else if !error}
  <p class="muted">Loading…</p>
{/if}
