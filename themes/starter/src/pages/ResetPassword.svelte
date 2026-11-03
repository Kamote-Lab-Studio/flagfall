<script lang="ts">
  import { errorText, ff, route } from "../lib/app.svelte";

  let { params: _ }: { params: Record<string, string> } = $props();

  const token = $derived(route.query.get("token") ?? "");
  let password = $state("");
  let error = $state("");
  let done = $state(false);

  async function submit(ev: SubmitEvent) {
    ev.preventDefault();
    try {
      await ff.auth.resetPassword({ token, password });
      done = true;
    } catch (err) {
      error = errorText(err);
    }
  }
</script>

<h1>Choose a new password</h1>
{#if done}
  <p role="status">Your password is changed. <a href="/sign-in">Sign in</a>.</p>
{:else if !token}
  <p class="error">This link is missing its token. Ask for a new one from the sign-in page.</p>
{:else}
  <form onsubmit={submit}>
    <label>New password <input type="password" autocomplete="new-password" required minlength="10" bind:value={password} /></label>
    {#if error}<p class="error" role="alert">{error}</p>{/if}
    <button>Set password</button>
  </form>
{/if}
