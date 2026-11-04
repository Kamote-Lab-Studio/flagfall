<script lang="ts">
  import { isError } from "@flagfall/theme-sdk";
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
  // Two-factor accounts: the ticket from the password step, and the code.
  let ticket = $state("");
  let code = $state("");

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
      await signedIn();
    } catch (err) {
      if (isError(err, "two_factor_required") && err.ticket) {
        ticket = err.ticket;
        code = "";
      } else {
        error = errorMessage(err, t);
      }
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
      error = errorMessage(err, t);
    } finally {
      busy = false;
    }
  }

  // After either sign-in step: go on to where the player was headed.
  async function signedIn() {
    const admin = destination();
    if (admin) {
      location.assign(admin); // a different app, so a full page load
      return;
    }
    await refreshMe();
    navigate(next(), { replace: true });
  }

  // Demo installs offer two public logins: a team captain for the player
  // site, and the owner for the admin panel. Picking one fills the form.
  type DemoKind = "player" | "organizer";
  const demos = $derived(
    [
      { kind: "player" as DemoKind, title: "Player", login: app.event?.demoAccount ?? null },
      { kind: "organizer" as DemoKind, title: "Organizer", login: app.event?.demoAdmin ?? null },
    ].filter((d) => d.login),
  );
  let picked = $state<DemoKind | null>(null);

  function useDemo(kind: DemoKind) {
    const d = demos.find((x) => x.kind === kind);
    if (!d?.login) return;
    picked = kind;
    email = d.login.email;
    password = d.login.password;
    error = "";
  }

  // Where to go after signing in: the admin panel for the demo organizer.
  function destination(): string | null {
    const admin = app.event?.demoAdmin;
    return admin && email.trim().toLowerCase() === admin.email.toLowerCase() ? "/admin" : null;
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
    {#if ticket}
      <form class="form-card" onsubmit={signInWithCode}>
        <h1>Two-factor sign-in</h1>
        <p class="muted">Enter the 6-digit code from your authenticator app, or one of your recovery codes.</p>
        <div class="field"><label for="code">Code</label><input id="code" class="mono" autocomplete="one-time-code" inputmode="text" spellcheck="false" required bind:value={code} /></div>
        {#if error}<p class="status-msg bad" role="alert">{error}</p>{/if}
        <button class="btn" type="submit" disabled={busy}>Sign in</button>
        <div class="form-foot"><button type="button" class="link" onclick={() => ((ticket = ""), (error = ""))}>Back</button></div>
      </form>
    {:else if forgot}
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
        {#if demos.length}
          <section class="demo" aria-labelledby="demo-title">
            <h2 id="demo-title">Try the demo</h2>
            <div class="demo-choices" role="radiogroup" aria-labelledby="demo-title">
              {#each demos as d (d.kind)}
                <button type="button" class="demo-choice" role="radio" aria-checked={picked === d.kind} onclick={() => useDemo(d.kind)}>
                  <b>{d.title}</b>
                </button>
              {/each}
            </div>
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
