<script lang="ts">
  import { app, errorText, ff, navigate, refreshMe } from "../lib/app.svelte";

  let { params: _ }: { params: Record<string, string> } = $props();

  let name = $state(app.me?.name ?? "");
  let teamName = $state("");
  let inviteCode = $state("");
  let current = $state("");
  let next = $state("");
  let message = $state<{ text: string; good: boolean } | null>(null);

  $effect(() => {
    if (app.ready && !app.me) navigate("/sign-in?next=/account");
  });

  // One helper for every form here: run it, show the outcome, refresh.
  async function run(ev: SubmitEvent | null, action: () => Promise<unknown>, done: string) {
    ev?.preventDefault();
    message = null;
    try {
      await action();
      await refreshMe();
      message = { text: done, good: true };
    } catch (err) {
      message = { text: errorText(err), good: false };
    }
  }

  function leave() {
    if (confirm("Leave your team?")) run(null, () => ff.teams.leave(), "You left the team.");
  }

  const bracketId = $derived(app.event?.brackets[0]?.id ?? "");
</script>

{#if app.me}
  <h1>Your account</h1>
  {#if message}<p class={message.good ? "ok" : "error"} role="status">{message.text}</p>{/if}

  <h2>Team</h2>
  {#if app.me.team}
    <p>You're on <a href="/teams/{app.me.team.id}">{app.me.team.name}</a>.</p>
    <p>Invite code: <code>{app.me.team.inviteCode}</code> <span class="muted">(share it with teammates)</span></p>
    <button type="button" class="secondary" onclick={leave}>Leave team</button>
  {:else}
    <form onsubmit={(ev) => run(ev, () => ff.teams.create({ name: teamName, bracketId }), "Team created.")}>
      <label>New team name <input required maxlength="40" bind:value={teamName} /></label>
      <button>Create team</button>
    </form>
    <form onsubmit={(ev) => run(ev, () => ff.teams.join(inviteCode.trim()), "You joined the team.")}>
      <label>Invite code <input required autocomplete="off" bind:value={inviteCode} /></label>
      <button>Join team</button>
    </form>
  {/if}

  <h2>Profile</h2>
  <form onsubmit={(ev) => run(ev, () => ff.auth.updateAccount({ name }), "Saved.")}>
    <label>Display name <input required maxlength="40" bind:value={name} /></label>
    <p class="muted">Email: {app.me.email}</p>
    <button>Save</button>
  </form>

  <h2>Password</h2>
  <form onsubmit={(ev) => run(ev, () => ff.auth.changePassword({ current, next }), "Password changed.")}>
    <label>Current password <input type="password" autocomplete="current-password" required bind:value={current} /></label>
    <label>New password <input type="password" autocomplete="new-password" required minlength="10" bind:value={next} /></label>
    <button>Change password</button>
  </form>
{/if}

<style>
  form + form { margin-top: 1rem; }
</style>
