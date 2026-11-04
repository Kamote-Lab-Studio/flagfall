<script lang="ts">
  import { formatDuration } from "@flagfall/theme-sdk";
  import { app, ff, serverNow, t } from "../lib/state.svelte";
  import { navigate, route } from "../lib/router.svelte";
  import { fmt, initials } from "../lib/util";
  import Logo from "./Logo.svelte";
  import Icon from "./Icon.svelte";

  const nav = [
    { href: "/", key: "nav.challenges", match: (p: string) => p === "/" || p.startsWith("/challenges") },
    { href: "/scoreboard", key: "nav.scoreboard", match: (p: string) => p === "/scoreboard" },
    { href: "/teams", key: "nav.teams", match: (p: string) => p.startsWith("/teams") },
    { href: "/p/rules", key: "nav.rules", match: (p: string) => p === "/p/rules" },
    { href: "/announcements", key: "nav.announcements", match: (p: string) => p === "/announcements" },
  ];

  // Splits e.g. "{time} left" around the countdown so it can be styled.
  const timer = $derived.by(() => {
    const ev = app.event;
    if (!ev) return null;
    const now = serverNow();
    const parts = (key: string, until: string) => {
      const mark = "\u0000";
      const [before = "", after = ""] = t(key, { time: mark }).split(mark);
      return { before, time: formatDuration(new Date(until).getTime() - now), after };
    };
    if (ev.state === "not_started" && ev.startsAt) return parts("header.startsIn", ev.startsAt);
    if (ev.state === "over") return { before: t("header.over"), time: "", after: "" };
    if (ev.endsAt) return parts("header.left", ev.endsAt);
    return null;
  });
  const team = $derived(app.me?.team ?? null);

  let signingOut = $state(false);
  async function signOut() {
    signingOut = true;
    try {
      await ff.auth.signOut();
      app.me = null;
      navigate("/sign-in");
    } finally {
      signingOut = false;
    }
  }
</script>

<header class="top">
  <div class="top-left">
    <Logo />
    <nav aria-label="Main">
      {#each nav as item}
        <a href={item.href} aria-current={item.match(route.path) ? "page" : undefined}>{t(item.key)}</a>
      {/each}
    </nav>
  </div>
  <div class="top-right">
    {#if timer}
      <span class="timer" role="timer">
        <span class="live-dot" class:open={app.liveOpen} title={app.liveOpen ? "Live" : "Reconnecting"}></span>
        <Icon name="clock" size={18} />
        {timer.before}{#if timer.time}<span class="mono">{timer.time}</span>{/if}{timer.after}
      </span>
    {/if}
    {#if app.me}
      <a class="me" href={team ? `/teams/${team.id}` : "/account"}>
        <span class="avatar me-av">{initials(team?.name ?? app.me.name)}</span>
        <span>
          <b>{team?.name ?? app.me.name}</b>
          <small>{team ? (team.rank ? t("header.rank", { rank: team.rank, points: fmt(team.score) }) : `${fmt(team.score)} pts`) : t("header.noTeam")}</small>
        </span>
      </a>
      <button type="button" class="btn-ghost sign-out" onclick={signOut} disabled={signingOut}>{t("header.signOut")}</button>
    {:else}
      <a class="btn" href="/sign-in">{t("header.signIn")}</a>
    {/if}
  </div>
</header>
