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

<h1>Event</h1>
{#if error}<p class="notice bad">{error}</p>{/if}
{#if ev}
  <form class="card" onsubmit={save}>
    <fieldset disabled={!editable}>
      <label><span>Name</span><input bind:value={form.name} maxlength="80" required />
        {#if errors.name}<div class="field-error">{errors.name}</div>{/if}</label>
      <div class="grid">
        <label><span>Starts</span><input type="datetime-local" bind:value={form.startsAt} />
          {#if errors.startsAt}<div class="field-error">{errors.startsAt}</div>{/if}</label>
        <label><span>Scoreboard freezes</span><input type="datetime-local" bind:value={form.freezeAt} />
          {#if errors.freezeAt}<div class="field-error">{errors.freezeAt}</div>{/if}</label>
        <label><span>Ends</span><input type="datetime-local" bind:value={form.endsAt} />
          {#if errors.endsAt}<div class="field-error">{errors.endsAt}</div>{/if}</label>
        <label><span>Time zone shown to players</span>
          <input bind:value={form.timeZone} list="zones" />
          <datalist id="zones">{#each zones as z (z)}<option value={z}></option>{/each}</datalist>
          {#if errors.timeZone}<div class="field-error">{errors.timeZone}</div>{/if}</label>
        <label><span>Max team size</span><input type="number" min="1" max="100" bind:value={form.maxTeamSize} />
          {#if errors.maxTeamSize}<div class="field-error">{errors.maxTeamSize}</div>{/if}</label>
        <label><span>Scoring</span>
          <select bind:value={form.scoringMode}><option value="static">Static</option><option value="dynamic">Dynamic</option></select>
          {#if errors.scoringMode}<div class="field-error">{errors.scoringMode}</div>{/if}</label>
        <label><span>Flag attempts per minute</span><input type="number" min="1" max="1000" bind:value={form.flagRateLimit} />
          {#if errors.flagRateLimit}<div class="field-error">{errors.flagRateLimit}</div>{/if}</label>
      </div>
      <p class="hint">Times are in your browser's time zone. Clear "freezes" to unfreeze the scoreboard.</p>
      <label class="check"><input type="checkbox" bind:checked={form.registrationOpen} /><span>Registration open</span></label>
      <label class="check"><input type="checkbox" bind:checked={form.teamsLocked} /><span>Teams locked (no joining, leaving or renaming)</span></label>
      <label class="check"><input type="checkbox" bind:checked={form.publicChallenges} /><span>Challenges visible without signing in</span></label>
      {#if editable}<div class="actions"><button class="primary" disabled={busy}>Save event</button>
        <span class="muted">Last changed {when(ev.updatedAt)} · state: {ev.state}</span></div>{/if}
    </fieldset>
  </form>

  <h2>Brackets</h2>
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
    <form class="toolbar" style="margin-top:0.75rem" onsubmit={addBracket}>
      <input placeholder="New bracket name" bind:value={newBracket.name} required />
      <input placeholder="Description" bind:value={newBracket.description} />
      <label class="check" style="margin:0"><input type="checkbox" bind:checked={newBracket.requiresVerification} /><span>Verified only</span></label>
      <button>Add bracket</button>
    </form>
  {/if}
{/if}

<style>
  fieldset { border: 0; margin: 0; padding: 0; min-width: 0; }
</style>
