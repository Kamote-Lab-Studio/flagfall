<script lang="ts">
  import type { Paged, TeamSummary } from "@flagfall/theme-sdk";
  import { app, ff, t } from "../lib/state.svelte";
  import { fmt, initials } from "../lib/util";

  let search = $state("");
  let recruiting = $state(false);
  let page = $state(1);
  let result = $state<Paged<TeamSummary> | null>(null);
  let timer: ReturnType<typeof setTimeout> | undefined;

  async function load() {
    result = await ff.teams.list({ search: search.trim() || undefined, recruiting: recruiting || undefined, page, pageSize: 48 });
  }

  $effect(() => {
    void recruiting;
    void page;
    void app.scoreVersion;
    load();
  });

  function onSearch() {
    clearTimeout(timer);
    timer = setTimeout(() => {
      page = 1;
      load();
    }, 250);
  }

  const pages = $derived(result ? Math.max(1, Math.ceil(result.total / result.pageSize)) : 1);
  const myId = $derived(app.me?.team?.id);
</script>

<main class="narrow">
  <div class="page-head">
    <div>
      <h1>{t("nav.teams")}</h1>
      <p class="muted">{result ? `${fmt(result.total)} teams registered.` : ""}</p>
    </div>
    <div class="filters">
      <label class="sr-only" for="team-search">Search teams</label>
      <input id="team-search" type="text" placeholder="Search teams" bind:value={search} oninput={onSearch} />
      <label class="check"><input type="checkbox" bind:checked={recruiting} onchange={() => (page = 1)} /> Recruiting only</label>
    </div>
  </div>

  {#if !result}
    <p class="loading">Loading…</p>
  {:else if result.items.length === 0}
    <p class="muted">No teams match.</p>
  {:else}
    <div class="team-grid">
      {#each result.items as team (team.id)}
        <a class="team-card" class:me-card={team.id === myId} href="/teams/{team.id}">
          <span class="team-id">
            <span class="avatar">{initials(team.name)}</span>
            <span><b>{team.name}</b><small>{team.country ?? ""}{team.recruiting ? " · recruiting" : ""}</small></span>
          </span>
          <span class="team-stats">
            <span>Rank<b>{team.rank ?? "–"}</b></span>
            <span>Points<b class="amber">{fmt(team.score)}</b></span>
            <span>Players<b>{team.memberCount}</b></span>
          </span>
          {#if team.id === myId}<span class="you">Your team</span>{/if}
        </a>
      {/each}
    </div>
    {#if pages > 1}
      <nav class="filters" aria-label="Pages">
        <button type="button" disabled={page <= 1} onclick={() => page--}>Previous</button>
        <span class="muted">Page {page} of {pages}</span>
        <button type="button" disabled={page >= pages} onclick={() => page++}>Next</button>
      </nav>
    {/if}
  {/if}
</main>
