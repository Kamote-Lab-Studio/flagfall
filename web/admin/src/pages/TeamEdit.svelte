<script lang="ts">
  import { onMount } from "svelte";
  import { api, fieldErrors, message, qs } from "../lib/api";
  import { href } from "../lib/router.svelte";
  import { can, notify } from "../lib/state.svelte";
  import type { Bracket, Paged, Team, TeamDetail, User } from "../lib/types";
  import { when } from "../lib/util";

  let { id }: { id: string } = $props();

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

<p><a href={href("/teams")}>← Teams</a></p>
{#if error}<p class="notice bad">{error}</p>{/if}
{#if d}
  <h1>{d.team.name}
    {#if d.team.hidden}<span class="badge warn">hidden</span>{/if}
    {#if d.team.banned}<span class="badge bad">banned</span>{/if}
  </h1>
  <p class="muted">Rank {d.team.rank ?? "—"} · {d.team.score} points · created {when(d.team.createdAt)}</p>

  <form class="card" onsubmit={save}>
    <div class="grid">
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
    <div class="actions">
      <button class="primary" disabled={busy}>Save team</button>
      <span class="muted">Invite code <code>{d.team.inviteCode}</code></span>
      <button type="button" class="small" onclick={() => act(() => api.post<TeamDetail>(`/teams/${id}/invite-code`), "New invite code made.")}>New code</button>
    </div>
  </form>

  <h2>Members</h2>
  <table>
    <tbody>
      {#each d.members as m (m.id)}
        <tr>
          <td><a href={href(`/users/${m.id}`)}>{m.name}</a> {#if m.id === d.team.captainId}<span class="badge">captain</span>{/if}</td>
          <td>{m.email}</td>
          <td class="num"><button class="small danger" onclick={() => removeMember(m)}>Remove</button></td>
        </tr>
      {:else}
        <tr><td class="empty">No members.</td></tr>
      {/each}
    </tbody>
  </table>
  <div class="toolbar" style="margin-top:0.75rem">
    <input type="search" placeholder="Find a user to add" aria-label="Find a user to add" bind:value={userSearch} oninput={findUsers} />
    {#each found.slice(0, 6) as u (u.id)}
      <button class="small" onclick={() => addMember(u)}>+ {u.name}{u.teamName ? ` (${u.teamName})` : ""}</button>
    {/each}
  </div>
  <p class="hint">Staff can add members past the team size limit.</p>

  <h2>Solves</h2>
  <table>
    <tbody>
      {#each d.solves as s (s.challengeId)}
        <tr><td><a href={href(`/challenges/${s.challengeId}`)}>{s.name}</a></td><td class="muted">{when(s.solvedAt)}</td></tr>
      {:else}
        <tr><td class="empty">No solves yet.</td></tr>
      {/each}
    </tbody>
  </table>
  <p><a href={href(`/submissions?teamId=${id}`)}>See all submissions →</a></p>

  {#if can("admin")}
    <h2>Merge another team into this one</h2>
    <div class="toolbar">
      <input type="search" placeholder="Find a team" aria-label="Find a team to merge" bind:value={mergeSearch} oninput={findTeams} />
      {#each mergeFound.slice(0, 6) as t (t.id)}
        <button class="small" onclick={() => merge(t)}>Merge {t.name}</button>
      {/each}
    </div>
    <p class="hint">Members and solves move here (the earlier solve wins); the other team is deleted.</p>
  {/if}
{/if}
