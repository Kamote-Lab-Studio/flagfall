<script lang="ts">
  import { isError } from "@flagfall/theme-sdk";
  import { app, errorText, ff, navigate, refreshMe, route } from "../lib/app.svelte";

  let { params: _ }: { params: Record<string, string> } = $props();

  let email = $state("");
  let password = $state("");
  let error = $state("");
  let busy = $state(false);
  let forgot = $state(false);
  let sent = $state(false);
  // Two-factor accounts: the ticket from the password step, and the code.
  let ticket = $state("");
  let code = $state("");

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
      await signedIn();
    } catch (err) {
      if (isError(err, "two_factor_required") && err.ticket) ticket = err.ticket;
      else error = errorText(err);
    } finally {
      busy = false;
    }
  }

  async function signInWithCode(ev: SubmitEvent) {
    ev.preventDefault();
    busy = true;
    error = "";
    try {
      await ff.auth.signInWithCode({ ticket, code });
      await signedIn();
    } catch (err) {
      if (isError(err, "ticket_expired")) ticket = "";
      error = errorText(err);
    } finally {
      busy = false;
    }
  }

  // After either sign-in step: go on to where the player was headed.
  async function signedIn() {
    // The demo organizer goes to the admin panel (a different app).
    if (app.event?.demoAdmin && email.toLowerCase() === app.event.demoAdmin.email.toLowerCase()) {
      location.assign("/admin");
      return;
    }
    await refreshMe();
    navigate(next());
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

{#if ticket}
  <h1>Two-factor sign-in</h1>
  <form onsubmit={signInWithCode}>
    <label>Code from your authenticator app, or a recovery code <input autocomplete="one-time-code" spellcheck="false" required bind:value={code} /></label>
    {#if error}<p class="error" role="alert">{error}</p>{/if}
    <button disabled={busy}>Sign in</button>
  </form>
  <p><button type="button" class="link" onclick={() => ((ticket = ""), (error = ""))}>Back</button></p>
{:else if forgot}
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
      {#if demoPlayer}<button type="button" class="secondary" onclick={() => fill(demoPlayer)}>Player</button>{/if}
      {#if demoAdmin}<button type="button" class="secondary" onclick={() => fill(demoAdmin)}>Organizer</button>{/if}
    </section>
  {/if}
{/if}

<style>
  .card { max-width: 26rem; }
  .card h2 { margin-top: 0; }
  .card button { margin: 0.25rem 0.25rem 0 0; }
</style>
