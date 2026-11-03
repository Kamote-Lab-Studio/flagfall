<script lang="ts">
  import { ff, t } from "../lib/state.svelte";
  import { route } from "../lib/router.svelte";
  import { errorMessage } from "../lib/util";
  import Logo from "../components/Logo.svelte";

  let password = $state("");
  let busy = $state(false);
  let done = $state(false);
  let error = $state("");
  const token = $derived(route.query.get("token") ?? "");

  async function submit(ev: SubmitEvent) {
    ev.preventDefault();
    busy = true;
    error = "";
    try {
      await ff.auth.resetPassword({ token, password });
      done = true;
    } catch (err) {
      error = errorMessage(err, t);
    } finally {
      busy = false;
    }
  }
</script>

<main class="split">
  <section class="split-left"><Logo /><p class="term-big">New password.<span class="cursor" aria-hidden="true"></span></p><span></span></section>
  <section class="split-right">
    <form class="form-card" onsubmit={submit}>
      <h1>Choose a new password</h1>
      {#if done}
        <p role="status">Password changed. Other sessions were signed out.</p>
        <a class="btn" href="/sign-in">Sign in</a>
      {:else if !token}
        <p class="status-msg bad">This link is missing its token. Request a new one from the sign-in page.</p>
        <a href="/sign-in">Back to sign in</a>
      {:else}
        <p class="muted">At least 10 characters.</p>
        <div class="field"><label for="password">New password</label><input id="password" type="password" autocomplete="new-password" minlength="10" required bind:value={password} /></div>
        {#if error}<p class="status-msg bad" role="alert">{error}</p>{/if}
        <button class="btn" type="submit" disabled={busy}>Set password</button>
      {/if}
    </form>
  </section>
</main>
