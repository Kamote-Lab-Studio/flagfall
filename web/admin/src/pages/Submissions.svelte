<script lang="ts">
  import { onMount } from "svelte";
  import { api, message, qs } from "../lib/api";
  import { href, route } from "../lib/router.svelte";
  import type { Submission } from "../lib/types";
  import { when } from "../lib/util";

  const PAGE = 100;
  let items = $state<Submission[]>([]);
  let more = $state(false);
  let error = $state("");
  let correct = $state("");
  let live = $state(true);
  // Filters from links (team and user pages).
  const teamId = route.query.get("teamId") ?? "";
  const userId = route.query.get("userId") ?? "";
  const challengeId = route.query.get("challengeId") ?? "";

  async function fetchPage(before?: string) {
    return api.get<Submission[]>("/submissions" + qs({ teamId, userId, challengeId, correct, before, limit: PAGE }));
  }

  async function load() {
    try {
      const page = await fetchPage();
      items = page;
      more = page.length === PAGE;
      error = "";
    } catch (err) {
      error = message(err);
    }
  }

  async function older() {
    const last = items.at(-1);
    if (!last) return;
    const page = await fetchPage(last.id);
    items = [...items, ...page];
    more = page.length === PAGE;
  }

  onMount(() => {
    load();
    const t = setInterval(() => live && items.length <= PAGE && load(), 10_000);
    return () => clearInterval(t);
  });
</script>

<h1>Submissions</h1>
{#if error}<p class="notice bad">{error}</p>{/if}
<div class="toolbar">
  <select bind:value={correct} onchange={load} aria-label="Result">
    <option value="">All</option><option value="false">Wrong only</option><option value="true">Correct only</option>
  </select>
  <label class="check" style="margin:0"><input type="checkbox" bind:checked={live} /><span>Refresh every 10 s</span></label>
  {#if teamId || userId || challengeId}<a href={href("/submissions")}>Clear filters</a>{/if}
</div>

<table>
  <thead><tr><th>When</th><th>Team</th><th>User</th><th>Challenge</th><th>Submitted</th><th>IP</th></tr></thead>
  <tbody>
    {#each items as s (s.id)}
      <tr>
        <td class="muted">{when(s.submittedAt)}</td>
        <td><a href={href(`/teams/${s.teamId}`)}>{s.teamName}</a></td>
        <td>{#if s.userId}<a href={href(`/users/${s.userId}`)}>{s.userName}</a>{:else}—{/if}</td>
        <td><a href={href(`/challenges/${s.challengeId}`)}>{s.challengeName}</a></td>
        <td class="mono">{#if s.correct}<span class="badge ok">correct</span>{:else}{s.submitted}{/if}</td>
        <td class="mono muted">{s.ip ?? "—"}</td>
      </tr>
    {:else}
      <tr><td colspan="6" class="empty">No submissions.</td></tr>
    {/each}
  </tbody>
</table>
{#if more}<div class="actions"><button onclick={older}>Older</button></div>{/if}
