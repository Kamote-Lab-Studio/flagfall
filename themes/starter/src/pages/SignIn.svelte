<script lang="ts">
  import { app, errorText, ff, navigate, refreshMe, route } from "../lib/app.svelte";

  let { params: _ }: { params: Record<string, string> } = $props();

  let email = $state("");
  let password = $state("");
  let error = $state("");
  let busy = $state(false);
  let forgot = $state(false);
  let sent = $state(false);

  // Only same-site paths, so ?next= can't send players elsewhere.
  function next(): string {
    const n = route.query.get("next") ?? "/";
    return n.startsWith("/") && !n.startsWith("//") ? n : "/";
  }

  async function signIn(ev: SubmitEvent) {
    ev.preventDefault();
    busy = true;
    error = "";
    try {
      await ff.auth.signIn({ email, password });
      // The demo organizer goes to the admin panel (a different app).
      if (app.event?.demoAdmin && email.toLowerCase() === app.event.demoAdmin.email.toLowerCase()) {
        location.assign("/admin");
        return;
      }
      await refreshMe();
      navigate(next());
    } catch (err) {
      error = errorText(err);
    } finally {
      busy = false;
    }
  }

  async function reset(ev: SubmitEvent) {
    ev.preventDefault();
    busy = true;
    try {
      await ff.auth.requestPasswordReset(email);
      sent = true;
    } catch (err) {
      error = errorText(err);
    } finally {
      busy = false;
    }
  }

  function fill(login: { email: string; password: string }) {
    email = login.email;
    password = login.password;
  }
</script>

{#if forgot}
  <h1>Reset your password</h1>
  {#if sent}
    <p role="status">If an account uses that email, a reset link is on its way.</p>
  {:else}
    <form onsubmit={reset}>
      <label>Email <input type="email" autocomplete="email" required bind:value={email} /></label>
      {#if error}<p class="error" role="alert">{error}</p>{/if}
      <button disabled={busy}>Send reset link</button>
    </form>
  {/if}
  <p><button type="button" class="link" onclick={() => ((forgot = false), (sent = false))}>Back to sign in</button></p>
{:else}
  <h1>Sign in</h1>
  <form onsubmit={signIn}>
    <label>Email <input type="email" autocomplete="email" required bind:value={email} /></label>
    <label>Password <input type="password" autocomplete="current-password" required bind:value={password} /></label>
    {#if error}<p class="error" role="alert">{error}</p>{/if}
    <button disabled={busy}>Sign in</button>
  </form>
  <p><button type="button" class="link" onclick={() => (forgot = true)}>Forgot your password?</button></p>
  {#if app.event?.registrationOpen}<p>New here? <a href="/sign-up">Create an account</a>.</p>{/if}

  {#if app.event?.demoAccount || app.event?.demoAdmin}
    {@const demoPlayer = app.event.demoAccount}
    {@const demoAdmin = app.event.demoAdmin}
    <section class="card" aria-labelledby="demo">
      <h2 id="demo">Try the demo</h2>
      <p>This is a demo install. Pick an account to fill in the form:</p>
      {#if demoPlayer}<button type="button" class="secondary" onclick={() => fill(demoPlayer)}>Player ({demoPlayer.email})</button>{/if}
      {#if demoAdmin}<button type="button" class="secondary" onclick={() => fill(demoAdmin)}>Organizer ({demoAdmin.email})</button>{/if}
    </section>
  {/if}
{/if}

<style>
  .card { max-width: 26rem; }
  .card h2 { margin-top: 0; }
  .card button { margin: 0.25rem 0.25rem 0 0; }
</style>
