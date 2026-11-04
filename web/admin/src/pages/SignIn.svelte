<script lang="ts">
  import { api, ApiError, message } from "../lib/api";

  let { onDone, demo = null }: { onDone: () => void; demo?: { email: string; password: string } | null } = $props();

  let email = $state("");
  let password = $state("");
  let error = $state("");
  let busy = $state(false);
  // Two-factor accounts: the ticket from the password step, and the code.
  let ticket = $state("");
  let code = $state("");

  async function submit(ev: SubmitEvent) {
    ev.preventDefault();
    busy = true;
    error = "";
    try {
      const r = await api.signIn(email, password);
      if (r?.twoFactorRequired && r.ticket) {
        ticket = r.ticket;
        code = "";
        return;
      }
      onDone();
    } catch (err) {
      error = message(err);
    } finally {
      busy = false;
    }
  }

  async function submitCode(ev: SubmitEvent) {
    ev.preventDefault();
    busy = true;
    error = "";
    try {
      await api.signInWithCode(ticket, code);
      onDone();
    } catch (err) {
      if (err instanceof ApiError && err.code === "ticket_expired") ticket = "";
      error = message(err);
    } finally {
      busy = false;
    }
  }

  function fillDemo() {
    if (!demo) return;
    email = demo.email;
    password = demo.password;
  }
</script>

<main>
  <h1>Flagfall admin</h1>

  {#if demo}
    <section class="card demo" aria-labelledby="demo-title">
      <h2 id="demo-title">Demo install</h2>
      <p>Try the admin panel with the demo owner account. Anyone can use it, so changes may not last.</p>
      <dl>
        <dt>Email</dt><dd class="mono">{demo.email}</dd>
        <dt>Password</dt><dd class="mono">{demo.password}</dd>
      </dl>
      <button type="button" onclick={fillDemo}>Fill in the demo account</button>
      <p class="switch">Want to play instead? <a href="/sign-in">Sign in as the demo player</a> on the event site.</p>
    </section>
  {/if}

  {#if ticket}
  <form class="card" onsubmit={submitCode}>
    <h2>Two-factor sign-in</h2>
    <p class="muted">Enter the 6-digit code from your authenticator app, or one of your recovery codes.</p>
    <label>
      <span>Code</span>
      <input class="mono" bind:value={code} autocomplete="one-time-code" spellcheck="false" required />
    </label>
    {#if error}<p class="field-error" role="alert">{error}</p>{/if}
    <div class="actions">
      <button class="primary" disabled={busy}>Sign in</button>
      <span class="spacer"></span>
      <button type="button" class="link" onclick={() => ((ticket = ""), (error = ""))}>Back</button>
    </div>
  </form>
  {:else}
  <form class="card" onsubmit={submit}>
    <label>
      <span>Email</span>
      <input type="email" bind:value={email} autocomplete="email" required />
    </label>
    <label>
      <span>Password</span>
      <input type="password" bind:value={password} autocomplete="current-password" required />
    </label>
    {#if error}<p class="field-error" role="alert">{error}</p>{/if}
    <div class="actions">
      <button class="primary" disabled={busy}>Sign in</button>
      <span class="spacer"></span>
      <a href="/forgot-password">Forgot your password?</a>
    </div>
  </form>
  {/if}
  <p class="muted"><a href="/">← Back to the event</a></p>
</main>

<style>
  main { max-width: 24rem; margin: 12vh auto; padding: 0 1rem; }
  .spacer { flex: 1; }
  .demo { border-color: color-mix(in srgb, var(--accent) 45%, var(--line)); }
  .demo h2 { margin: 0 0 0.4rem; font-size: 0.95rem; }
  .demo p { color: var(--muted); font-size: 0.9rem; }
  .demo .switch { margin: 0.75rem 0 0; }
  dl { display: grid; grid-template-columns: auto 1fr; gap: 0.15rem 0.75rem; margin: 0 0 0.75rem; }
  dt { color: var(--muted); font-size: 0.85rem; }
  dd { margin: 0; overflow-wrap: anywhere; }
</style>
