<script lang="ts">
  import { api, message } from "../lib/api";

  let { onDone }: { onDone: () => void } = $props();

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
</script>

<main>
  <h1>Flagfall admin</h1>
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
  main { max-width: 24rem; margin: 15vh auto; padding: 0 1rem; }
  .spacer { flex: 1; }
</style>
