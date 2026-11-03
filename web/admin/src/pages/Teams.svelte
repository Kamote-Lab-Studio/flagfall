<script lang="ts">
  import { onMount } from "svelte";
  import { api, message, qs } from "../lib/api";
  import { href } from "../lib/router.svelte";
  import type { Paged, Team } from "../lib/types";
  import Pager from "../components/Pager.svelte";

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

<h1>Teams</h1>
{#if error}<p class="notice bad">{error}</p>{/if}
<div class="toolbar">
  <input type="search" placeholder="Search team name" aria-label="Search teams" bind:value={search} oninput={onSearch} />
</div>

{#if data}
  <table>
    <thead><tr><th class="num">Rank</th><th>Team</th><th>Country</th><th class="num">Members</th><th class="num">Score</th><th>Status</th></tr></thead>
    <tbody>
      {#each data.items as t (t.id)}
        <tr>
          <td class="num">{t.rank ?? "—"}</td>
          <td><a href={href(`/teams/${t.id}`)}>{t.name}</a></td>
          <td>{t.country ?? "—"}</td>
          <td class="num">{t.memberCount}</td>
          <td class="num">{t.score}</td>
          <td>
            {#if t.hidden}<span class="badge warn">hidden</span>{/if}
            {#if t.banned}<span class="badge bad">banned</span>{/if}
            {#if !t.prizeEligible}<span class="badge">no prizes</span>{/if}
          </td>
        </tr>
      {:else}
        <tr><td colspan="6" class="empty">No teams found.</td></tr>
      {/each}
    </tbody>
  </table>
  <Pager page={data.page} pageSize={data.pageSize} total={data.total} onPage={(p) => { page = p; load(); }} />
{/if}
