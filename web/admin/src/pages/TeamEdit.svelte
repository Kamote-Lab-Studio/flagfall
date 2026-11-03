<script lang="ts">
  import { onMount } from "svelte";
  import { api, fieldErrors, message, qs } from "../lib/api";
  import { href } from "../lib/router.svelte";
  import { can, notify } from "../lib/state.svelte";
  import type { Bracket, Paged, Team, TeamDetail, User } from "../lib/types";
  import { when } from "../lib/util";

  // Shown beside the team list; onchange lets the list refresh.
  let { id, onchange }: { id: string; onchange?: () => void } = $props();

  let d = $state<TeamDetail | null>(null);
  let brackets = $state<Bracket[]>([]);
  let form = $state({ name: "", country: "", bracketId: "", hidden: false, banned: false, prizeEligible: true, captainId: "" });
  let errors = $state<Record<string, string>>({});
  let error = $state("");
  let busy = $state(false);

  let userSearch = $state("");
  let found = $state<User[]>([]);
  let mergeSearch = $state("");
  let mergeFound = $state<Team[]>([]);

  function fill(x: TeamDetail) {
    d = x;
    const t = x.team;
    form = {
      name: t.name, country: t.country ?? "", bracketId: t.bracketId, hidden: t.hidden, banned: t.banned,
      prizeEligible: t.prizeEligible, captainId: t.captainId ?? "",
    };
  }

  async function load() {
    try {
      fill(await api.get<TeamDetail>(`/teams/${id}`));
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
      fill(await api.patch<TeamDetail>(`/teams/${id}`, {
        ...form, country: form.country || null, captainId: form.captainId || null,
      }));
      notify("Team saved.");
      onchange?.();
    } catch (err) {
      errors = fieldErrors(err);
      notify(message(err), "error");
    } finally {
      busy = false;
    }
  }

  async function act(fn: () => Promise<TeamDetail>, done?: string) {
    try {
      fill(await fn());
      if (done) notify(done);
      onchange?.();
    } catch (err) {
      notify(message(err), "error");
    }
  }

  async function findUsers() {
    found = userSearch ? (await api.get<Paged<User>>("/users" + qs({ search: userSearch }))).items.filter((u) => u.teamId !== id) : [];
  }

  async function findTeams() {
    mergeFound = mergeSearch ? (await api.get<Paged<Team>>("/teams" + qs({ search: mergeSearch }))).items.filter((t) => t.id !== id) : [];
  }

  function addMember(u: User) {
    const note = u.teamName ? ` They'll leave ${u.teamName}.` : "";
    if (!confirm(`Move ${u.name} into this team?${note}`)) return;
    act(() => api.post<TeamDetail>(`/teams/${id}/members`, { userId: u.id }), `${u.name} added.`);
    userSearch = "";
    found = [];
  }

  function removeMember(u: User) {
    if (!confirm(`Remove ${u.name} from the team?`)) return;
    act(() => api.del<TeamDetail>(`/teams/${id}/members/${u.id}`), `${u.name} removed.`);
  }

  function merge(t: Team) {
    if (!confirm(`Merge "${t.name}" into this team? Its members and solves move here and "${t.name}" is deleted.`)) return;
    act(() => api.post<TeamDetail>(`/teams/${id}/merge`, { fromTeamId: t.id }), "Teams merged.");
    mergeSearch = "";
    mergeFound = [];
  }

  onMount(load);
</script>

{#if error}<p class="notice bad">{error}</p>{/if}
{#if d}
  <form class="panel pad" onsubmit={save} aria-labelledby="team-title">
    <div>
      <div class="panel-head">
        <h2 id="team-title">{d.team.name}</h2>
        <a class="small link" href={href("/teams")} aria-label="Close the editor">Close</a>
      </div>
      <p class="muted small">
        {#if d.team.country}<span class="mono">{d.team.country}</span>{" "}{/if}Rank {d.team.rank ?? "—"}, {d.team.score} points, {d.solves.length} {d.solves.length === 1 ? "solve" : "solves"}. Created {when(d.team.createdAt)}.
      </p>
      {#if d.team.hidden || d.team.banned}<div class="badges" style="margin-top:6px">
        {#if d.team.hidden}<span class="badge warn">Hidden</span>{/if}
        {#if d.team.banned}<span class="badge bad">Banned</span>{/if}
      </div>{/if}
    </div>
    <div class="grid2">
      <label><span>Name</span><input bind:value={form.name} required />
        {#if errors.name}<div class="field-error">{errors.name}</div>{/if}</label>
      <label><span>Country (ISO code)</span><input bind:value={form.country} maxlength="2" />
        {#if errors.country}<div class="field-error">{errors.country}</div>{/if}</label>
      <label><span>Bracket</span>
        <select bind:value={form.bracketId}>{#each brackets as b (b.id)}<option value={b.id}>{b.name}</option>{/each}</select>
        {#if errors.bracketId}<div class="field-error">{errors.bracketId}</div>{/if}</label>
      <label><span>Captain</span>
        <select bind:value={form.captainId}>
          <option value="">—</option>
          {#each d.members as m (m.id)}<option value={m.id}>{m.name}</option>{/each}
        </select>
        {#if errors.captainId}<div class="field-error">{errors.captainId}</div>{/if}</label>
    </div>
    <label class="check"><input type="checkbox" bind:checked={form.hidden} /><span>Hidden from the public scoreboard</span></label>
    <label class="check"><input type="checkbox" bind:checked={form.banned} /><span>Banned</span></label>
    <label class="check"><input type="checkbox" bind:checked={form.prizeEligible} /><span>Eligible for prizes</span></label>
    <div class="code-row">
      <code>{d.team.inviteCode}</code>
      <button type="button" class="small" onclick={() => act(() => api.post<TeamDetail>(`/teams/${id}/invite-code`), "New invite code made.")}>Reset code</button>
    </div>
    <div class="actions">
      <button class="primary grow" disabled={busy}>Save team</button>
      <a class="btn-line" href={"/teams/" + id} target="_blank" rel="noopener">Public team page</a>
    </div>
  </form>

  <section class="panel pad stack" aria-labelledby="members-title">
  <h2 id="members-title">Players</h2>
  <ul class="member-list">
    {#each d.members as m (m.id)}
      <li>
        <span><a href={href(`/users/${m.id}`)}>{m.name}</a> {#if m.id === d.team.captainId}<span class="badge">captain</span>{/if}</span>
        <button class="small danger" onclick={() => removeMember(m)}>Remove</button>
      </li>
    {:else}
      <li class="muted">No players.</li>
    {/each}
  </ul>
  <div class="toolbar">
    <input type="search" placeholder="Find a user to add" aria-label="Find a user to add" bind:value={userSearch} oninput={findUsers} />
    {#each found.slice(0, 6) as u (u.id)}
      <button class="small" onclick={() => addMember(u)}>+ {u.name}{u.teamName ? ` (${u.teamName})` : ""}</button>
    {/each}
  </div>
  <p class="hint">Staff can add members past the team size limit.</p>
  </section>

  <section class="panel table-panel" aria-labelledby="solves-title">
  <h2 id="solves-title">Solves</h2>
  <table>
    <tbody>
      {#each d.solves as s (s.challengeId)}
        <tr><td><a href={href(`/challenges/${s.challengeId}`)}>{s.name}</a></td><td class="muted">{when(s.solvedAt)}</td></tr>
      {:else}
        <tr><td class="empty">No solves yet.</td></tr>
      {/each}
    </tbody>
  </table>
  <p style="padding: 8px 24px 14px"><a href={href(`/submissions?teamId=${id}`)}>See all submissions</a></p>
  </section>

  {#if can("admin")}
    <section class="panel pad stack" aria-labelledby="merge-title">
    <h2 id="merge-title">Merge another team into this one</h2>
    <div class="toolbar">
      <input type="search" placeholder="Find a team" aria-label="Find a team to merge" bind:value={mergeSearch} oninput={findTeams} />
      {#each mergeFound.slice(0, 6) as t (t.id)}
        <button class="small" onclick={() => merge(t)}>Merge {t.name}</button>
      {/each}
    </div>
    <p class="hint">Members and solves move here (the earlier solve wins); the other team is deleted.</p>
    </section>
  {/if}
{/if}
