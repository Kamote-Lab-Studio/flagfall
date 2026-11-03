<script lang="ts">
  import { onMount } from "svelte";
  import { app, applySettings, ff, refreshEvent, refreshMe, t, toast } from "./lib/state.svelte";
  import { installLinkHandler, match, route } from "./lib/router.svelte";
  import en from "../messages/en.json";
  import fil from "../messages/fil.json";

  import Header from "./components/Header.svelte";
  import Toasts from "./components/Toasts.svelte";
  import Board from "./pages/Board.svelte";
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

  // Paths must match "pages" in theme.json.
  const routes: [string, any, boolean?][] = [
    ["/", Board],
    ["/challenges", Board],
    ["/challenges/:slug", Board],
    ["/scoreboard", Scoreboard],
    ["/teams", Teams],
    ["/teams/:id", Team],
    ["/p/:slug", ContentPage],
    ["/sign-in", SignIn, true],
    ["/sign-up", SignUp],
    ["/account", Account],
    ["/announcements", Announcements],
    ["/starting-soon", NotStarted],
    ["/ended", Over],
    ["/reset-password", ResetPassword, true],
  ];

  const current = $derived.by(() => {
    for (const [pattern, component, bare] of routes) {
      const params = match(pattern, route.path);
      if (params) return { component, params, bare: !!bare, pattern };
    }
    return { component: NotFound, params: {}, bare: false, pattern: "" };
  });

  // Pages stay mounted while only their params change (e.g. picking another
  // challenge keeps the board), and react to params themselves.

  // Event-state screens replace the board before the start and after the end.
  const view = $derived.by(() => {
    const state = app.event?.state;
    if (state === "not_started" && (current.pattern === "/" || current.pattern.startsWith("/challenges"))) return { ...current, component: NotStarted };
    if (state === "over" && current.pattern === "/") return { ...current, component: Over };
    return current;
  });

  onMount(() => {
    installLinkHandler();
    const tick = setInterval(() => (app.now = Date.now()), 1000);

    (async () => {
      try {
        const [theme] = await Promise.all([ff.loadTheme(), refreshEvent(), refreshMe()]);
        theme.addMessages("en", en);
        theme.addMessages("fil", fil);
        app.theme = theme;
        applySettings(theme.settings);
        document.documentElement.lang = theme.locale;
      } catch (err) {
        console.error("Flagfall: failed to load", err);
        toast("Can't reach the server", "Retrying when the connection is back.", true);
      } finally {
        app.ready = true;
      }
    })();

    ff.live.onStatus((s) => (app.liveOpen = s === "open"));
    ff.live.on("scoreboard", () => {
      app.scoreVersion++;
      if (app.me?.team) refreshMe();
    });
    ff.live.on("solve", (s) => {
      if (s.firstBlood) toast("First blood", `${s.teamName} solved ${s.challengeSlug}`);
      app.challengeVersion++;
    });
    ff.live.on("challenge-updated", () => app.challengeVersion++);
    ff.live.on("announcement", (a) => {
      toast(a.title, "New announcement");
      app.announcementVersion++;
    });
    ff.live.on("event-state", () => refreshEvent());
    ff.live.on("resync", () => {
      refreshEvent();
      refreshMe();
      app.scoreVersion++;
      app.challengeVersion++;
      app.announcementVersion++;
    });

    // The countdown hitting zero changes the state; refetch so screens switch.
    let lastState = "";
    const stateCheck = setInterval(() => {
      const ev = app.event;
      if (!ev) return;
      const now = app.now + app.clockOffset;
      const due = (iso: string | null) => iso && now >= new Date(iso).getTime();
      const key = `${ev.state}:${due(ev.startsAt)}:${due(ev.endsAt)}:${due(ev.freezeAt)}`;
      if (lastState && key !== lastState) refreshEvent();
      lastState = key;
    }, 1000);

    return () => {
      clearInterval(tick);
      clearInterval(stateCheck);
      ff.live.close();
    };
  });

  $effect(() => {
    const name = app.event?.name ?? "Flagfall";
    document.title = name;
  });
</script>

{#if app.theme?.preview}
  <div class="banner preview">Preview mode. Players don't see these changes until you publish them.</div>
{/if}

{#if !app.ready}
  <p class="loading">Loading…</p>
{:else if view.bare}
  {#key view.component}
    <view.component params={view.params} />
  {/key}
{:else}
  <Header />
  {#key view.component}
    <view.component params={view.params} />
  {/key}
{/if}

<Toasts />

