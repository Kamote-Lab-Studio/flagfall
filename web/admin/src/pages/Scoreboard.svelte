<script lang="ts">
  import { onMount } from "svelte";
  import { api, fieldErrors, message } from "../lib/api";
  import { href } from "../lib/router.svelte";
  import { can, notify } from "../lib/state.svelte";
  import type { Adjustment, Board } from "../lib/types";
  import { download, when } from "../lib/util";

  let board = $state<Board | null>(null);
  let adjustments = $state<Adjustment[]>([]);
  let error = $state("");
  let adj = $state({ teamId: "", points: 0, reason: "" });
  let errors = $state<Record<string, string>>({});

  async function load() {
    try {
      [board, adjustments] = await Promise.all([api.get<Board>("/scoreboard"), api.get<Adjustment[]>("/scoreboard/adjustments")]);
    } catch (err) {
      error = message(err);
    }
  }

  async function addAdjustment(ev: SubmitEvent) {
    ev.preventDefault();
    errors = {};
    try {
      await api.post("/scoreboard/adjustments", adj);
      adj = { teamId: "", points: 0, reason: "" };
      notify("Adjustment added.");
      await load();
    } catch (err) {
      errors = fieldErrors(err);
      notify(message(err), "error");
    }
  }

  async function removeAdjustment(a: Adjustment) {
    if (!confirm(`Remove the ${a.points > 0 ? "+" : ""}${a.points} adjustment for ${a.teamName}?`)) return;
    try {
      await api.del(`/scoreboard/adjustments/${a.id}`);
      await load();
    } catch (err) {
      notify(message(err), "error");
    }
  }

  async function exportCTFtime() {
    try {
      download("ctftime.json", await api.get("/scoreboard/export/ctftime"));
    } catch (err) {
      notify(message(err), "error");
    }
  }

  onMount(load);
</script>

<h1>Scoreboard</h1>
{#if error}<p class="notice bad">{error}</p>{/if}
{#if board}
  <div class="toolbar">
    {#if board.frozen}<span class="badge warn">frozen since {when(board.freezeAt)}</span>
      <span class="muted">You see the live board; players see it as of the freeze.</span>{/if}
    <span class="spacer"></span>
    <button onclick={exportCTFtime}>Export for CTFtime</button>
  </div>
  <table>
    <thead><tr><th class="num">Rank</th><th>Team</th><th class="num">Score</th><th class="num">Solves</th><th class="num">First bloods</th><th>Last solve</th><th></th></tr></thead>
    <tbody>
      {#each board.entries as e (e.teamId)}
        <tr class:off={e.rank === null}>
          <td class="num">{e.rank ?? "—"}</td>
          <td><a href={href(`/teams/${e.teamId}`)}>{e.teamName}</a></td>
          <td class="num">{e.score}</td>
          <td class="num">{e.solveCount}</td>
          <td class="num">{e.firstBloods}</td>
          <td class="muted">{when(e.lastSolveAt)}</td>
          <td>{#if e.hidden}<span class="badge warn">hidden</span>{/if}{#if e.banned}<span class="badge bad">banned</span>{/if}</td>
        </tr>
      {:else}
        <tr><td colspan="7" class="empty">No teams yet.</td></tr>
      {/each}
    </tbody>
  </table>
  <p class="hint">Hidden and banned teams are listed without a rank. Updated {when(board.updatedAt)}.</p>

  <h2>Adjustments</h2>
  <table>
    <thead><tr><th>Team</th><th class="num">Points</th><th>Reason</th><th>By</th><th>When</th><th></th></tr></thead>
    <tbody>
      {#each adjustments as a (a.id)}
        <tr>
          <td>{a.teamName}</td>
          <td class="num">{a.points > 0 ? "+" : ""}{a.points}</td>
          <td>{a.reason}</td>
          <td>{a.actorName ?? "—"}</td>
          <td class="muted">{when(a.createdAt)}</td>
          <td class="num">{#if can("admin")}<button class="small danger" onclick={() => removeAdjustment(a)}>Remove</button>{/if}</td>
        </tr>
      {:else}
        <tr><td colspan="6" class="empty">No adjustments.</td></tr>
      {/each}
    </tbody>
  </table>
  {#if can("admin")}
    <form class="toolbar" style="margin-top:0.75rem" onsubmit={addAdjustment}>
      <select bind:value={adj.teamId} aria-label="Team" required>
        <option value="" disabled>Team…</option>
        {#each board.entries as e (e.teamId)}<option value={e.teamId}>{e.teamName}</option>{/each}
      </select>
      <input type="number" bind:value={adj.points} aria-label="Points (negative to take away)" style="min-width:6rem;width:6rem" />
      <input bind:value={adj.reason} placeholder="Reason (required, players don't see it)" aria-label="Reason" required style="flex:1" />
      <button>Add adjustment</button>
    </form>
    {#each Object.values(errors) as e (e)}<div class="field-error">{e}</div>{/each}
  {/if}
{/if}

<style>
  tr.off td { color: var(--muted); }
</style>
