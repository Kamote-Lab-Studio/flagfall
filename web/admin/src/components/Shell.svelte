<script lang="ts">
  import { href, match, route } from "../lib/router.svelte";
  import { can, session } from "../lib/state.svelte";
  import type { Role } from "../lib/types";
  import Dashboard from "../pages/Dashboard.svelte";
  import EventPage from "../pages/Event.svelte";
  import Challenges from "../pages/Challenges.svelte";
  import Users from "../pages/Users.svelte";
  import Teams from "../pages/Teams.svelte";
  import Announcements from "../pages/Announcements.svelte";
  import Scoreboard from "../pages/Scoreboard.svelte";
  import Submissions from "../pages/Submissions.svelte";
  import Account from "../pages/Account.svelte";
  import Pages from "../pages/Pages.svelte";
  import Audit from "../pages/Audit.svelte";
  import Data from "../pages/Data.svelte";
  import Settings from "../pages/Settings.svelte";
  import Appearance from "../pages/Appearance.svelte";
  import TopBar from "./TopBar.svelte";

  let { onSignOut }: { onSignOut: () => void } = $props();

  // Sections from design/option-a/admin. "Users and teams" covers both lists.
  const nav: { path: string; label: string; role: Role; also?: string[] }[] = [
    { path: "/", label: "Dashboard", role: "support" },
    { path: "/event", label: "Event", role: "support" },
    { path: "/challenges", label: "Challenges", role: "support" },
    { path: "/teams", label: "Users and teams", role: "support", also: ["/users"] },
    { path: "/submissions", label: "Submissions", role: "support" },
    { path: "/announcements", label: "Announcements", role: "support" },
    { path: "/pages", label: "Pages", role: "support" },
    { path: "/appearance", label: "Appearance", role: "admin" },
    { path: "/scoreboard", label: "Scoreboard", role: "support" },
    { path: "/data", label: "Data", role: "admin" },
    { path: "/settings", label: "Settings", role: "owner" },
    { path: "/audit", label: "Audit log", role: "admin" },
  ];

  function under(path: string) {
    return path === "/" ? route.path === "/" : route.path === path || route.path.startsWith(path + "/");
  }
  const active = (n: (typeof nav)[number]) => under(n.path) || (n.also ?? []).some(under);

  const params = $derived.by(() => {
    for (const p of ["/challenges/:id", "/users/:id", "/teams/:id"]) {
      const m = match(p, route.path);
      if (m) return { pattern: p, id: m.id ?? "" };
    }
    return null;
  });
</script>

<div class="adm">
  <aside class="side">
    <a class="logo" href={href("/")}>
      <svg aria-hidden="true" width="26" height="26" viewBox="0 0 64 64"><path d="M16 8c-5 0-7 2-7 7v9c0 4-2 6-5 8 3 2 5 4 5 8v9c0 5 2 7 7 7" fill="none" stroke="var(--accent)" stroke-width="6" stroke-linecap="round" stroke-linejoin="round"/><path d="M48 8c5 0 7 2 7 7v9c0 4 2 6 5 8-3 2-5 4-5 8v9c0 5-2 7-7 7" fill="none" stroke="var(--accent)" stroke-width="6" stroke-linecap="round" stroke-linejoin="round"/><path d="M24 18h5v30h-5ZM29 21 L45 28 L29 35Z" fill="var(--text)"/></svg>
      <span class="wordmark">flag<b>{"{"}</b>fall<b>{"}"}</b></span>
      <span class="admin-tag">Admin</span>
    </a>
    <nav aria-label="Admin sections">
      {#each nav.filter((n) => can(n.role)) as n (n.path)}
        <a href={href(n.path)} aria-current={active(n) ? "page" : undefined}>{n.label}</a>
      {/each}
    </nav>
    <div class="side-foot">
      <a href="/" class="link" target="_blank" rel="noopener">View player site</a>
      <span class="muted">Signed in as {session.me?.name}, {session.me?.role}</span>
      <a href={href("/account")} class="link" aria-current={route.path === "/account" ? "page" : undefined}>Account security</a>
      <button class="small" onclick={onSignOut}>Sign out</button>
    </div>
  </aside>

  <div class="adm-main">
    <TopBar />
    <main class="adm-content">
      {#if route.path === "/"}
        <Dashboard />
      {:else if route.path === "/event"}
        <EventPage />
      {:else if route.path === "/challenges" || params?.pattern === "/challenges/:id"}
        <Challenges selected={params?.pattern === "/challenges/:id" ? params.id : null} />
      {:else if route.path === "/users" || params?.pattern === "/users/:id"}
        <Users selected={params?.pattern === "/users/:id" ? params.id : null} />
      {:else if route.path === "/teams" || params?.pattern === "/teams/:id"}
        <Teams selected={params?.pattern === "/teams/:id" ? params.id : null} />
      {:else if route.path === "/announcements"}
        <Announcements />
      {:else if route.path === "/scoreboard"}
        <Scoreboard />
      {:else if route.path === "/submissions"}
        {#key route.query.toString()}<Submissions />{/key}
      {:else if route.path === "/pages"}
        <Pages />
      {:else if route.path === "/appearance" && can("admin")}
        <Appearance />
      {:else if route.path === "/settings" && can("owner")}
        <Settings />
      {:else if route.path === "/account"}
        <Account />
      {:else if route.path === "/data" && can("admin")}
        <Data />
      {:else if route.path === "/audit" && can("admin")}
        <Audit />
      {:else}
        <h1>Not found</h1>
        <p><a href={href("/")}>Back to the dashboard</a></p>
      {/if}
    </main>
  </div>
</div>

<style>
  .adm { display: flex; min-height: 100vh; }
  .side { flex: none; width: 250px; position: sticky; top: 0; height: 100vh; overflow-y: auto; padding: 20px 16px; background: var(--deep); border-right: 1px solid var(--line); display: flex; flex-direction: column; gap: 20px; }
  .logo { display: flex; align-items: center; gap: 10px; padding: 4px 8px; font-size: 20px; color: var(--text); }
  .logo svg { flex: none; }
  .wordmark { font-family: "JetBrains Mono", var(--mono); font-weight: 800; letter-spacing: -0.02em; }
  .wordmark b { color: var(--accent); font-weight: 800; }
  .admin-tag { margin-left: 4px; padding: 1px 8px; border: 1px solid var(--accent-line); border-radius: 10px; color: var(--accent-2); font-size: 12px; font-family: var(--sans); }
  nav { display: flex; flex-direction: column; gap: 2px; }
  nav a { display: flex; align-items: center; min-height: 40px; padding: 0 14px; border-radius: var(--radius); color: var(--text-2); }
  nav a:hover { color: var(--accent); }
  nav a[aria-current="page"] { background: var(--panel-2); color: var(--accent); font-weight: 600; }
  .side-foot { margin-top: auto; display: flex; flex-direction: column; align-items: flex-start; gap: 8px; padding: 12px 8px 0; border-top: 1px solid var(--line); font-size: 14px; }
  .adm-main { flex: 1; min-width: 0; display: flex; flex-direction: column; }
  .adm-content { flex: 1; width: 100%; max-width: 1240px; padding: 28px 32px 48px; display: flex; flex-direction: column; gap: 24px; }
  @media (max-width: 860px) {
    .adm { flex-direction: column; }
    .side { position: static; width: auto; height: auto; }
    nav { flex-direction: row; flex-wrap: wrap; }
    .adm-content { padding: 20px 20px 40px; }
  }
</style>
