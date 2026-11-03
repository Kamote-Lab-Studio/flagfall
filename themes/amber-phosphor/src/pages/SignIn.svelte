<script lang="ts">
  import { app, ff, refreshMe, setting, t } from "../lib/state.svelte";
  import { navigate, route } from "../lib/router.svelte";
  import { errorMessage } from "../lib/util";
  import Logo from "../components/Logo.svelte";

  let email = $state("");
  let password = $state("");
  let busy = $state(false);
  let error = $state("");
  let forgot = $state(false);
  let sent = $state(false);

  function next(): string {
    const n = route.query.get("next") ?? "/";
    return n.startsWith("/") && !n.startsWith("//") ? n : "/";
  }

  $effect(() => {
    if (app.me) navigate(next(), { replace: true });
  });

  async function signIn(ev: SubmitEvent) {
    ev.preventDefault();
    busy = true;
    error = "";
    try {
      await ff.auth.signIn({ email, password });
      await refreshMe();
      navigate(next(), { replace: true });
    } catch (err) {
      error = errorMessage(err, t);
    } finally {
      busy = false;
    }
  }

  const demo = $derived(app.event?.demoAccount ?? null);

  function useDemo() {
    if (!demo) return;
    email = demo.email;
    password = demo.password;
    error = "";
  }

  async function reset(ev: SubmitEvent) {
    ev.preventDefault();
    busy = true;
    error = "";
    try {
      await ff.auth.requestPasswordReset(email);
      sent = true;
    } catch (err) {
      error = errorMessage(err, t);
    } finally {
      busy = false;
    }
  }
</script>

<main class="split">
  <section class="split-left">
    <Logo />
    <div>
      <pre class="term">$ ssh player@{location.host}
connected to {app.event?.name ?? "flagfall"}
</pre>
      <p class="term-big">{setting("signInHeadline") || "Capture the flag."}<span class="cursor" aria-hidden="true"></span></p>
    </div>
    <p class="muted">{setting("organizer")}</p>
  </section>

  <section class="split-right">
    {#if forgot}
      <form class="form-card" onsubmit={reset}>
        <h1>Reset password</h1>
        {#if sent}
          <p role="status">If an account uses that email, a reset link is on its way. Check your inbox.</p>
        {:else}
          <p class="muted">We'll email you a link to choose a new password.</p>
          <div class="field"><label for="email">Email</label><input id="email" type="email" autocomplete="email" required bind:value={email} /></div>
          {#if error}<p class="status-msg bad" role="alert">{error}</p>{/if}
          <button class="btn" type="submit" disabled={busy}>Send reset link</button>
        {/if}
        <div class="form-foot"><button type="button" class="link" onclick={() => ((forgot = false), (sent = false))}>Back to sign in</button></div>
      </form>
    {:else}
      <form class="form-card" onsubmit={signIn}>
        <h1>Sign in</h1>
        <p class="muted">Welcome back. Your team is waiting.</p>
        <div class="field"><label for="email">Email</label><input id="email" type="email" autocomplete="email" required bind:value={email} /></div>
        <div class="field">
          <div class="field-row"><label for="password">Password</label><button type="button" class="link" onclick={() => (forgot = true)}>Forgot it?</button></div>
          <input id="password" type="password" autocomplete="current-password" required bind:value={password} />
        </div>
        {#if error}<p class="status-msg bad" role="alert">{error}</p>{/if}
        <button class="btn" type="submit" disabled={busy}>Sign in</button>
        {#if demo}
          <section class="demo" aria-labelledby="demo-title">
            <h2 id="demo-title">Demo account</h2>
            <p>This is a demo install. Sign in as a team captain to try it:</p>
            <dl>
              <dt>Email</dt><dd><code>{demo.email}</code></dd>
              <dt>Password</dt><dd><code>{demo.password}</code></dd>
            </dl>
            <button type="button" class="btn-ghost" onclick={useDemo}>Fill in the demo account</button>
          </section>
        {/if}
        <div class="form-foot">
          {#if app.event?.registrationOpen}<span>New here? <a href="/sign-up">Create an account</a></span>{/if}
          <a href="/">Browse without signing in</a>
        </div>
      </form>
    {/if}
  </section>
</main>
