<script lang="ts">
  import { relativeTime, type MyTeam, type Team } from "@flagfall/theme-sdk";
  import { app, ff, refreshMe, t, toast } from "../lib/state.svelte";
  import { navigate } from "../lib/router.svelte";
  import { errorMessage, fmt, formatDate, initials } from "../lib/util";

  let { params }: { params: { id?: string } } = $props();

  let team = $state<Team | MyTeam | null>(null);
  let notFound = $state(false);
  let confirmLeave = $state(false);

  async function load(id: string) {
    try {
      team = await ff.teams.get(id);
      notFound = false;
    } catch {
      team = null;
      notFound = true;
    }
  }

  $effect(() => {
    void app.scoreVersion;
    if (params.id) load(params.id);
  });

  const mine = $derived(team && "inviteCode" in team ? (team as MyTeam) : null);
  const maxCat = $derived(Math.max(1, ...(team?.scoreByCategory.map((c) => c.score) ?? [])));
  const bracket = $derived(app.event?.brackets.find((b) => b.id === team?.bracketId)?.name ?? "");

  async function act(fn: () => Promise<unknown>, done?: string) {
    try {
      await fn();
      if (done) toast(done);
      await refreshMe();
      if (params.id) await load(params.id);
    } catch (err) {
      toast("That didn't work", errorMessage(err, t), true);
    }
  }

  async function leave() {
    if (!confirmLeave) {
      confirmLeave = true;
      return;
    }
    await act(() => ff.teams.leave(), "You left the team");
    navigate("/account");
  }

  async function copyCode() {
    if (!mine) return;
    await navigator.clipboard?.writeText(mine.inviteCode);
    toast("Invite code copied");
  }
</script>

{#if notFound}
  <main class="narrow"><h1>Team not found</h1><p class="muted">It may have been renamed or removed. <a href="/teams">All teams</a></p></main>
{:else if !team}
  <p class="loading">Loading…</p>
{:else}
  <main class="narrow stack">
    <a class="back" href="/teams">← All teams</a>
    <section class="team-hero" aria-labelledby="team-name">
      <div class="team-id big">
        <span class="avatar">{initials(team.name)}</span>
        <span>
          <h1 id="team-name">{team.name}</h1>
          <p class="muted">{[team.country, bracket, `since ${formatDate(team.createdAt, { weekday: undefined, hour: undefined, minute: undefined })}`].filter(Boolean).join(" · ")}</p>
        </span>
      </div>
      <dl class="hero-stats">
        <div><dt>Rank</dt><dd>{team.rank ?? "–"}</dd></div>
        <div><dt>Points</dt><dd class="amber">{fmt(team.score)}</dd></div>
        <div><dt>Solves</dt><dd>{team.solves.length}</dd></div>
        <div><dt>First bloods</dt><dd>{team.firstBloodCount}</dd></div>
      </dl>
    </section>

    <div class="two-col">
      <div class="col-main">
        <section aria-labelledby="solves-title">
          <h2 id="solves-title" class="h2">Solves</h2>
          <ul class="solves">
            {#each team.solves as s (s.challengeSlug)}
              <li>
                <span class="mono muted">{s.category.toLowerCase()}</span>
                <span><a href="/challenges/{s.challengeSlug}">{s.challengeName}</a>{#if s.firstBlood} <span class="tag">First blood</span>{/if}<br /><small class="muted">{s.userName}</small></span>
                <span class="muted">{relativeTime(s.solvedAt, t)}</span>
                <span class="pts">{s.points}</span>
              </li>
            {:else}
              <li><span class="empty">No solves yet.</span></li>
            {/each}
          </ul>
        </section>
      </div>

      <div class="col-side">
        <section class="panel" aria-labelledby="players-title">
          <h2 id="players-title">Players <span class="muted">{team.members.length} of {app.event?.maxTeamSize ?? "?"}</span></h2>
          <ul class="members">
            {#each team.members as m (m.id)}
              <li>
                <span class="who"><span class="avatar">{initials(m.name)}</span><span>{m.name}{#if m.role === "captain"} <span class="tag">Captain</span>{/if}<br /><small class="muted">{m.solveCount} solves · {fmt(m.score)} pts</small></span></span>
                {#if mine?.isCaptain && m.id !== app.me?.id}
                  <span class="row-actions">
                    <button type="button" class="btn-ghost" onclick={() => act(() => ff.teams.transferCaptain(m.id), `${m.name} is now captain`)}>Make captain</button>
                    <button type="button" class="btn-danger" onclick={() => act(() => ff.teams.removeMember(m.id), `${m.name} was removed`)}>Remove</button>
                  </span>
                {/if}
              </li>
            {/each}
          </ul>
        </section>

        {#if mine}
          <section class="panel" aria-labelledby="invite-title">
            <h2 id="invite-title">Invite code</h2>
            <p class="muted">Share it with teammates so they can join from the sign-up page.</p>
            <div class="code-box"><code>{mine.inviteCode}</code></div>
            <div class="row-actions">
              <button type="button" class="btn-ghost" onclick={copyCode}>Copy</button>
              {#if mine.isCaptain}<button type="button" class="btn-ghost" onclick={() => act(() => ff.teams.regenerateInviteCode(), "New invite code made")}>New code</button>{/if}
            </div>
            {#if mine.isCaptain}
              <label class="check"><input type="checkbox" checked={mine.recruiting} onchange={(e) => act(() => ff.teams.update({ recruiting: e.currentTarget.checked }))} /> Show the team as recruiting</label>
            {/if}
          </section>
        {/if}

        {#if team.scoreByCategory.length}
          <section class="panel" aria-labelledby="cat-title">
            <h2 id="cat-title">Points by category</h2>
            {#each team.scoreByCategory as c}
              <div class="catrow"><span class="mono muted">{c.category.toLowerCase()}</span><span class="cbar"><span style:width="{(c.score / maxCat) * 100}%"></span></span><span class="num">{fmt(c.score)}</span></div>
            {/each}
          </section>
        {/if}

        {#if mine}
          <button type="button" class="btn-danger" onclick={leave}>{confirmLeave ? "Click again to leave the team" : "Leave team"}</button>
        {/if}
      </div>
    </div>
  </main>
{/if}
