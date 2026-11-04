<script lang="ts">
  import { app, ff, refreshMe, t, toast } from "../lib/state.svelte";
  import { navigate } from "../lib/router.svelte";
  import { errorMessage } from "../lib/util";

  let name = $state(app.me?.name ?? "");
  let country = $state(app.me?.country ?? "");
  let current = $state("");
  let next = $state("");
  let code = $state("");

  $effect(() => {
    if (app.ready && !app.me) navigate("/sign-in?next=/account", { replace: true });
  });

  async function run(fn: () => Promise<unknown>, done: string) {
    try {
      await fn();
      toast(done);
      await refreshMe();
      return true;
    } catch (err) {
      toast("That didn't work", errorMessage(err, t), true);
      return false;
    }
  }

  const saveProfile = (e: SubmitEvent) => {
    e.preventDefault();
    run(() => ff.auth.updateAccount({ name, country: country || null }), "Profile saved");
  };
  const savePassword = async (e: SubmitEvent) => {
    e.preventDefault();
    if (await run(() => ff.auth.changePassword({ current, next }), "Password changed")) current = next = "";
  };
  const join = async (e: SubmitEvent) => {
    e.preventDefault();
    if (await run(() => ff.teams.join(code.trim()), "You joined the team") && app.me?.team) navigate(`/teams/${app.me.team.id}`);
  };
  async function signOut() {
    await ff.auth.signOut();
    app.me = null;
    navigate("/sign-in");
  }
</script>

{#if app.me}
  <main class="narrow stack">
    <div class="page-head"><h1>Your account</h1><button type="button" class="btn-ghost" onclick={signOut}>Sign out</button></div>
    <div class="account-grid">
      <form class="panel stack" onsubmit={saveProfile}>
        <h2>Profile</h2>
        <div class="field"><label for="name">Display name</label><input id="name" type="text" required maxlength="40" bind:value={name} /></div>
        <div class="field"><label for="country">Country code</label><input id="country" type="text" maxlength="2" bind:value={country} /></div>
        <div class="field"><span class="label">Email</span><span class="muted">{app.me.email}{app.me.verified ? "" : " (not verified)"}</span></div>
        <button class="btn" type="submit">Save</button>
      </form>

      <form class="panel stack" onsubmit={savePassword}>
        <h2>Password</h2>
        <div class="field"><label for="cur">Current password</label><input id="cur" type="password" autocomplete="current-password" required bind:value={current} /></div>
        <div class="field"><label for="new">New password</label><input id="new" type="password" autocomplete="new-password" minlength="10" required bind:value={next} /><small>At least 10 characters.</small></div>
        <button class="btn" type="submit">Change password</button>
      </form>

      <section class="panel stack">
        <h2>Team</h2>
        {#if app.me.team}
          <p>You play for <a href="/teams/{app.me.team.id}">{app.me.team.name}</a>.</p>
        {:else}
          <p class="muted">You're not on a team yet.</p>
          <form class="stack" onsubmit={join}>
            <div class="field"><label for="code">Invite code</label><input id="code" class="code-input" type="text" required bind:value={code} /></div>
            <button class="btn" type="submit">Join team</button>
          </form>
          <a href="/sign-up">Or create a new team</a>
        {/if}
      </section>
    </div>
  </main>
{/if}
