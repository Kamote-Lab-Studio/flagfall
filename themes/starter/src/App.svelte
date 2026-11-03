<script lang="ts">
  import { onMount } from "svelte";
  import { app, ff, installLinks, match, navigate, route, setting, start } from "./lib/app.svelte";

  import Board from "./pages/Board.svelte";
  import Challenge from "./pages/Challenge.svelte";
  import Scoreboard from "./pages/Scoreboard.svelte";
  import Teams from "./pages/Teams.svelte";
  import Team from "./pages/Team.svelte";
  import ContentPage from "./pages/ContentPage.svelte";
  import SignIn from "./pages/SignIn.svelte";
  import SignUp from "./pages/SignUp.svelte";
  import Account from "./pages/Account.svelte";
  import Announcements from "./pages/Announcements.svelte";
  import NotStarted from "./pages/NotStarted.svelte";
  import Over from "./pages/Over.svelte";
  import ResetPassword from "./pages/ResetPassword.svelte";
  import NotFound from "./pages/NotFound.svelte";

  // Every path here must match "pages" in theme.json.
  const routes: [string, any][] = [
    ["/", Board],
    ["/challenges/:slug", Challenge],
    ["/scoreboard", Scoreboard],
    ["/teams", Teams],
    ["/teams/:id", Team],
    ["/p/:slug", ContentPage],
    ["/sign-in", SignIn],
    ["/sign-up", SignUp],
    ["/account", Account],
    ["/announcements", Announcements],
    ["/starting-soon", NotStarted],
    ["/ended", Over],
    ["/reset-password", ResetPassword],
  ];

  const current = $derived.by(() => {
    for (const [pattern, component] of routes) {
      const params = match(pattern, route.path);
      if (params) {
        // Before the start and after the end, the board shows the event state.
        const state = app.event?.state;
        if (pattern === "/" && state === "not_started") return { component: NotStarted, params };
        if (pattern === "/" && state === "over") return { component: Over, params };
        return { component, params };
      }
    }
    return { component: NotFound, params: {} };
  });

  let error = $state("");

  onMount(() => {
    installLinks();
    start()
      .catch(() => (error = "Can't reach the server. Reload the page to try again."))
      .finally(() => (app.ready = true));
    return () => ff.live.close();
  });

  $effect(() => {
    document.title = app.event?.name ?? "Flagfall";
  });

  async function signOut() {
    await ff.auth.signOut();
    app.me = null;
    navigate("/");
  }
</script>

<a class="skip" href="#main">Skip to content</a>

{#if app.theme?.preview}
  <p class="preview" role="status">Preview mode. Players don't see these changes until you publish them.</p>
{/if}

<header>
  <a class="brand" href="/">
    {#if setting("logo")}<img src={setting("logo")} alt="" width="28" height="28" />{/if}
    {app.event?.name ?? "Flagfall"}
  </a>
  <nav aria-label="Main">
    <a href="/" aria-current={route.path === "/" ? "page" : undefined}>Challenges</a>
    <a href="/scoreboard" aria-current={route.path === "/scoreboard" ? "page" : undefined}>Scoreboard</a>
    <a href="/teams" aria-current={route.path.startsWith("/teams") ? "page" : undefined}>Teams</a>
    <a href="/announcements" aria-current={route.path === "/announcements" ? "page" : undefined}>Announcements</a>
  </nav>
  <div class="who">
    {#if app.me}
      <a href="/account">{app.me.name}</a>
      <button type="button" class="link" onclick={signOut}>Sign out</button>
    {:else if app.ready}
      <a href="/sign-in">Sign in</a>
    {/if}
  </div>
</header>

<main id="main">
  {#if error}
    <p class="error" role="alert">{error}</p>
  {:else if !app.ready}
    <p>Loading…</p>
  {:else}
    {#key current.component}
      <current.component params={current.params} />
    {/key}
  {/if}
</main>
