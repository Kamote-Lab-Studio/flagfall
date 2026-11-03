<script lang="ts">
  import { onMount } from "svelte";
  import { api, message, qs } from "../lib/api";
  import { href } from "../lib/router.svelte";
  import type { Paged, Team } from "../lib/types";
  import Pager from "../components/Pager.svelte";
  import PeopleTabs from "../components/PeopleTabs.svelte";
  import TeamEdit from "./TeamEdit.svelte";

  // The list, with the selected team's editor beside it (design/option-a/admin/team-*.html).
  let { selected = null }: { selected?: string | null } = $props();

  let data = $state<Paged<Team> | null>(null);
  let search = $state("");
  let page = $state(1);
  let error = $state("");

  async function load() {
    try {
      data = await api.get<Paged<Team>>("/teams" + qs({ search, page }));
      error = "";
    } catch (err) {
      error = message(err);
    }
  }

  let timer: ReturnType<typeof setTimeout> | undefined;
  function onSearch() {
    clearTimeout(timer);
    timer = setTimeout(() => {
      page = 1;
      load();
    }, 250);
  }

  onMount(load);
</script>

<PeopleTabs />
{#if error}<p class="notice bad">{error}</p>{/if}

<div class="row">
  <section class="grow-main" aria-label="Team list">
    <div class="toolbar">
      <input type="search" placeholder="Search team name" aria-label="Search teams" bind:value={search} oninput={onSearch} />
      {#if data}<span class="muted small">{data.total} {data.total === 1 ? "team" : "teams"}</span>{/if}
    </div>
    {#if data}
      <div class="table-wrap">
        <table>
          <thead><tr><th scope="col" class="num">Rank</th><th scope="col">Team</th><th scope="col">Country</th><th scope="col" class="num">Players</th><th scope="col" class="num">Points</th><th scope="col">Status</th></tr></thead>
          <tbody>
            {#each data.items as t (t.id)}
              <tr class:sel={t.id === selected}>
                <td class="num mono muted">{t.rank ?? "—"}</td>
                <td><a href={href(`/teams/${t.id}`)} aria-current={t.id === selected ? "page" : undefined}>{t.name}</a></td>
                <td class="mono muted">{t.country ?? "—"}</td>
                <td class="num mono">{t.memberCount}</td>
                <td class="num mono">{t.score}</td>
                <td><div class="badges">
                  {#if t.hidden}<span class="badge warn">Hidden</span>{/if}
                  {#if t.banned}<span class="badge bad">Banned</span>{/if}
                  {#if !t.prizeEligible}<span class="badge">No prizes</span>{/if}
                  {#if !t.hidden && !t.banned && t.prizeEligible}<span class="badge ok">OK</span>{/if}
                </div></td>
              </tr>
            {:else}
              <tr><td colspan="6" class="empty">No teams found.</td></tr>
            {/each}
          </tbody>
        </table>
      </div>
      <Pager page={data.page} pageSize={data.pageSize} total={data.total} onPage={(p) => { page = p; load(); }} />
    {/if}
  </section>

  {#if selected}
    <div class="side-panel stack">
      {#key selected}<TeamEdit id={selected} onchange={load} />{/key}
    </div>
  {/if}
</div>
