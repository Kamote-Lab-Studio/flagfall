<script lang="ts">
  import { app, errorText, ff, navigate, refreshMe } from "../lib/app.svelte";

  let { params: _ }: { params: Record<string, string> } = $props();

  let name = $state("");
  let email = $state("");
  let password = $state("");
  let error = $state("");
  let fields = $state<Record<string, string>>({});
  let busy = $state(false);

  async function submit(ev: SubmitEvent) {
    ev.preventDefault();
    busy = true;
    error = "";
    fields = {};
    try {
      await ff.auth.signUp({ name, email, password });
      await refreshMe();
      navigate("/account"); // next: create or join a team
    } catch (err) {
      error = errorText(err);
      fields = (err as { fields?: Record<string, string> }).fields ?? {};
    } finally {
      busy = false;
    }
  }
</script>

<h1>Create an account</h1>
{#if app.event && !app.event.registrationOpen}
  <p>Registration is closed.</p>
{:else}
  <form onsubmit={submit}>
    <label>Display name <input autocomplete="nickname" required maxlength="40" bind:value={name} /></label>
    {#if fields.name}<p class="error">{fields.name}</p>{/if}
    <label>Email <input type="email" autocomplete="email" required bind:value={email} /></label>
    {#if fields.email}<p class="error">{fields.email}</p>{/if}
    <label>Password <input type="password" autocomplete="new-password" required minlength="10" bind:value={password} /></label>
    {#if fields.password}<p class="error">{fields.password}</p>{/if}
    {#if error}<p class="error" role="alert">{error}</p>{/if}
    <button disabled={busy}>Create account</button>
  </form>
  <p>Already have an account? <a href="/sign-in">Sign in</a>.</p>
{/if}
