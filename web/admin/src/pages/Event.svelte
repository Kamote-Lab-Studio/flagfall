<script lang="ts">
  import { onMount } from "svelte";
  import { api, fieldErrors, message } from "../lib/api";
  import { can, notify } from "../lib/state.svelte";
  import type { Bracket, Event } from "../lib/types";
  import { fromLocalInput, toLocalInput, when } from "../lib/util";

  let ev = $state<Event | null>(null);
  let brackets = $state<Bracket[]>([]);
  let form = $state({
    name: "", startsAt: "", endsAt: "", freezeAt: "", timeZone: "", maxTeamSize: 4, scoringMode: "static",
    registrationOpen: true, teamsLocked: false, publicChallenges: false, flagRateLimit: 10,
  });
  let errors = $state<Record<string, string>>({});
  let error = $state("");
  let busy = $state(false);
  let newBracket = $state({ name: "", description: "", requiresVerification: false });
  const editable = $derived(can("admin"));

  function fill(e: Event) {
    ev = e;
    form = {
      name: e.name, startsAt: toLocalInput(e.startsAt), endsAt: toLocalInput(e.endsAt), freezeAt: toLocalInput(e.freezeAt),
      timeZone: e.timeZone, maxTeamSize: e.maxTeamSize, scoringMode: e.scoringMode, registrationOpen: e.registrationOpen,
      teamsLocked: e.teamsLocked, publicChallenges: e.publicChallenges, flagRateLimit: e.flagRateLimit,
    };
  }

  async function load() {
    try {
      fill(await api.get<Event>("/event"));
      brackets = await api.get<Bracket[]>("/brackets");
    } catch (err) {
      error = message(err);
    }
  }

  async function save(ev: SubmitEvent) {
    ev.preventDefault();
    busy = true;
    errors = {};
    try {
      fill(await api.patch<Event>("/event", {
        ...form,
        startsAt: fromLocalInput(form.startsAt), endsAt: fromLocalInput(form.endsAt), freezeAt: fromLocalInput(form.freezeAt),
      }));
      notify("Event saved.");
    } catch (err) {
      errors = fieldErrors(err);
      notify(message(err), "error");
    } finally {
      busy = false;
    }
  }

  async function addBracket(e: SubmitEvent) {
    e.preventDefault();
    try {
      await api.post("/brackets", { ...newBracket, position: brackets.length });
      newBracket = { name: "", description: "", requiresVerification: false };
      brackets = await api.get<Bracket[]>("/brackets");
    } catch (err) {
      notify(message(err), "error");
    }
  }

  async function saveBracket(b: Bracket) {
    try {
      await api.patch(`/brackets/${b.id}`, { name: b.name, description: b.description, requiresVerification: b.requiresVerification });
      notify("Bracket saved.");
    } catch (err) {
      notify(message(err), "error");
    }
  }

  async function deleteBracket(b: Bracket) {
    if (!confirm(`Delete the bracket "${b.name}"?`)) return;
    try {
      await api.del(`/brackets/${b.id}`);
      brackets = brackets.filter((x) => x.id !== b.id);
    } catch (err) {
      notify(message(err), "error");
    }
  }

  const zones = typeof Intl.supportedValuesOf === "function" ? Intl.supportedValuesOf("timeZone") : [];

  onMount(load);
</script>

<form class="stack" onsubmit={save}>
  <div class="page-head">
    <div>
      <h1>Event settings</h1>
      <p class="muted">Changes to dates and scoring apply to everyone at once. They're recorded in the audit log.</p>
    </div>
    {#if editable && ev}
      <div class="adm-actions">
        <button class="primary" disabled={busy}>Save settings</button>
      </div>
    {/if}
  </div>
  {#if error}<p class="notice bad">{error}</p>{/if}
  {#if ev}
    <fieldset disabled={!editable} class="cards2">
      <section class="panel pad stack" aria-labelledby="ev-event">
        <h2 id="ev-event">Event</h2>
        <label><span>Event name</span><input bind:value={form.name} maxlength="80" required />
          {#if errors.name}<div class="field-error">{errors.name}</div>{/if}</label>
        <div class="grid2">
          <label><span>Starts</span><input type="datetime-local" bind:value={form.startsAt} />
            {#if errors.startsAt}<div class="field-error">{errors.startsAt}</div>{/if}</label>
          <label><span>Ends</span><input type="datetime-local" bind:value={form.endsAt} />
            {#if errors.endsAt}<div class="field-error">{errors.endsAt}</div>{/if}</label>
        </div>
        <label><span>Time zone shown to players</span>
          <input bind:value={form.timeZone} list="zones" />
          <datalist id="zones">{#each zones as z (z)}<option value={z}></option>{/each}</datalist>
          {#if errors.timeZone}<div class="field-error">{errors.timeZone}</div>{/if}</label>
        <p class="hint">Times are in your browser's time zone. Players can switch to their local time.</p>
      </section>

      <section class="panel pad stack" aria-labelledby="ev-teams">
        <h2 id="ev-teams">Teams and registration</h2>
        <label><span>Max players per team</span><input type="number" min="1" max="100" bind:value={form.maxTeamSize} />
          {#if errors.maxTeamSize}<div class="field-error">{errors.maxTeamSize}</div>{/if}</label>
        <div>
          <label class="toggle-row">
            <span><span class="label">Registration open</span><span class="muted small">New players and teams can sign up.</span></span>
            <input type="checkbox" bind:checked={form.registrationOpen} />
          </label>
          <label class="toggle-row">
            <span><span class="label">Teams locked</span><span class="muted small">Nobody can join, leave or rename a team.</span></span>
            <input type="checkbox" bind:checked={form.teamsLocked} />
          </label>
          <label class="toggle-row">
            <span><span class="label">Challenges visible without signing in</span><span class="muted small">Visitors can read challenges but not submit flags.</span></span>
            <input type="checkbox" bind:checked={form.publicChallenges} />
          </label>
        </div>
      </section>

      <section class="panel pad stack" aria-labelledby="ev-scoring">
        <h2 id="ev-scoring">Scoring</h2>
        <div class="stack-s" role="radiogroup" aria-label="Scoring mode">
          <label class="radio"><input type="radio" value="static" bind:group={form.scoringMode} />
            <span>Static<small class="muted">Each challenge is always worth its listed points.</small></span></label>
          <label class="radio"><input type="radio" value="dynamic" bind:group={form.scoringMode} />
            <span>Dynamic<small class="muted">Points drop as more teams solve, down to a minimum.</small></span></label>
          {#if errors.scoringMode}<div class="field-error">{errors.scoringMode}</div>{/if}
        </div>
        <label><span>Flag attempts per minute</span><input type="number" min="1" max="1000" bind:value={form.flagRateLimit} />
          {#if errors.flagRateLimit}<div class="field-error">{errors.flagRateLimit}</div>{/if}</label>
        <p class="hint">Per team.</p>
      </section>

      <section class="panel pad stack" aria-labelledby="ev-board">
        <h2 id="ev-board">Scoreboard</h2>
        <label><span>Freeze the public scoreboard at</span><input type="datetime-local" bind:value={form.freezeAt} />
          {#if errors.freezeAt}<div class="field-error">{errors.freezeAt}</div>{/if}</label>
        <p class="hint">Teams still see their own score. Clear the time to unfreeze.</p>
        <p class="muted small">Last changed {when(ev.updatedAt)}. State: {ev.state}.</p>
      </section>
    </fieldset>
  {/if}
</form>

{#if ev}
  <section class="panel table-panel" aria-labelledby="ev-brackets">
  <h2 id="ev-brackets">Brackets</h2>
  <table>
    <thead><tr><th>Name</th><th>Description</th><th>Verified only</th><th class="num">Teams</th><th></th></tr></thead>
    <tbody>
      {#each brackets as b (b.id)}
        <tr>
          <td><input aria-label="Bracket name" bind:value={b.name} disabled={!editable} /></td>
          <td><input aria-label="Description" bind:value={b.description} disabled={!editable} /></td>
          <td><input type="checkbox" aria-label="Requires verification" bind:checked={b.requiresVerification} disabled={!editable} /></td>
          <td class="num">{b.teams}</td>
          <td>{#if editable}<div class="actions" style="margin:0">
            <button class="small" onclick={() => saveBracket(b)}>Save</button>
            <button class="small danger" onclick={() => deleteBracket(b)} disabled={b.teams > 0 || brackets.length === 1}>Delete</button>
          </div>{/if}</td>
        </tr>
      {/each}
    </tbody>
  </table>
  {#if editable}
    <form class="toolbar" style="padding: 12px 24px 14px" onsubmit={addBracket}>
      <input placeholder="New bracket name" bind:value={newBracket.name} required />
      <input placeholder="Description" bind:value={newBracket.description} />
      <label class="check" style="margin:0"><input type="checkbox" bind:checked={newBracket.requiresVerification} /><span>Verified only</span></label>
      <button>Add bracket</button>
    </form>
  {/if}
  </section>
{/if}

<style>
  .radio { display: flex; gap: 10px; align-items: flex-start; }
  .radio small { display: block; }
  .toggle-row .label { display: block; color: var(--text); }
</style>
