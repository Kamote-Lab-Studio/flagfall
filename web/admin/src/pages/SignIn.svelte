<script lang="ts">
  import { api, message } from "../lib/api";

  let { onDone, demo = null }: { onDone: () => void; demo?: { email: string; password: string } | null } = $props();

  let email = $state("");
  let password = $state("");
  let error = $state("");
  let busy = $state(false);

  async function submit(ev: SubmitEvent) {
    ev.preventDefault();
    busy = true;
    error = "";
    try {
      await api.signIn(email, password);
      onDone();
    } catch (err) {
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
    </section>
  {/if}

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
  <p class="muted"><a href="/">← Back to the event</a></p>
</main>

<style>
  main { max-width: 24rem; margin: 12vh auto; padding: 0 1rem; }
  .spacer { flex: 1; }
  .demo { border-color: color-mix(in srgb, var(--accent) 45%, var(--line)); }
  .demo h2 { margin: 0 0 0.4rem; font-size: 0.95rem; }
  .demo p { color: var(--muted); font-size: 0.9rem; }
  dl { display: grid; grid-template-columns: auto 1fr; gap: 0.15rem 0.75rem; margin: 0 0 0.75rem; }
  dt { color: var(--muted); font-size: 0.85rem; }
  dd { margin: 0; overflow-wrap: anywhere; }
</style>
