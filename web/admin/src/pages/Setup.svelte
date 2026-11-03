<script lang="ts">
  import { api, fieldErrors, message } from "../lib/api";

  let { onDone }: { onDone: () => void } = $props();

  let form = $state({ setupCode: "", name: "", email: "", password: "", eventName: "", loadSample: false });
  let errors = $state<Record<string, string>>({});
  let error = $state("");
  let busy = $state(false);

  async function submit(ev: SubmitEvent) {
    ev.preventDefault();
    busy = true;
    errors = {};
    error = "";
    try {
      await api.post("/setup", { ...form, setupCode: form.setupCode.trim().toUpperCase() });
      onDone();
    } catch (err) {
      errors = fieldErrors(err);
      if (!Object.keys(errors).length) error = message(err);
    } finally {
      busy = false;
    }
  }
</script>

<main>
  <h1>Set up Flagfall</h1>
  <p class="muted">Create the owner account and name your event. You can change everything later.</p>
  <form class="card" onsubmit={submit}>
    <label>
      <span>Setup code</span>
      <input class="mono" bind:value={form.setupCode} autocomplete="off" required />
      <div class="hint">It's printed in the server log when Flagfall starts.</div>
      {#if errors.setupCode}<div class="field-error">{errors.setupCode}</div>{/if}
    </label>
    <label>
      <span>Event name</span>
      <input bind:value={form.eventName} required maxlength="80" />
      {#if errors.eventName}<div class="field-error">{errors.eventName}</div>{/if}
    </label>
    <h2>Owner account</h2>
    <label>
      <span>Name</span>
      <input bind:value={form.name} autocomplete="username" required />
      {#if errors.name}<div class="field-error">{errors.name}</div>{/if}
    </label>
    <label>
      <span>Email</span>
      <input type="email" bind:value={form.email} autocomplete="email" required />
      {#if errors.email}<div class="field-error">{errors.email}</div>{/if}
    </label>
    <label>
      <span>Password</span>
      <input type="password" bind:value={form.password} autocomplete="new-password" required minlength="12" />
      {#if errors.password}<div class="field-error">{errors.password}</div>{/if}
    </label>
    <label class="check">
      <input type="checkbox" bind:checked={form.loadSample} />
      <span>Load the sample challenges (to try things out)</span>
    </label>
    {#if error}<p class="field-error" role="alert">{error}</p>{/if}
    <div class="actions"><button class="primary" disabled={busy}>Create owner and continue</button></div>
  </form>
</main>

<style>
  main { max-width: 32rem; margin: 8vh auto; padding: 0 1rem; }
</style>
