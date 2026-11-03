<script lang="ts">
  import { href, match, route } from "../lib/router.svelte";
  import { can, session } from "../lib/state.svelte";
  import type { Role } from "../lib/types";
  import Dashboard from "../pages/Dashboard.svelte";
  import EventPage from "../pages/Event.svelte";
  import Challenges from "../pages/Challenges.svelte";
  import ChallengeEdit from "../pages/ChallengeEdit.svelte";
  import Users from "../pages/Users.svelte";
  import UserEdit from "../pages/UserEdit.svelte";
  import Teams from "../pages/Teams.svelte";
  import TeamEdit from "../pages/TeamEdit.svelte";
  import Announcements from "../pages/Announcements.svelte";
  import Scoreboard from "../pages/Scoreboard.svelte";
  import Submissions from "../pages/Submissions.svelte";
  import Pages from "../pages/Pages.svelte";
  import Audit from "../pages/Audit.svelte";
  import Data from "../pages/Data.svelte";
  import Settings from "../pages/Settings.svelte";
  import Appearance from "../pages/Appearance.svelte";

  let { onSignOut }: { onSignOut: () => void } = $props();

  const nav: { path: string; label: string; role: Role }[] = [
    { path: "/", label: "Dashboard", role: "support" },
    { path: "/event", label: "Event", role: "support" },
    { path: "/challenges", label: "Challenges", role: "support" },
    { path: "/users", label: "Users", role: "support" },
    { path: "/teams", label: "Teams", role: "support" },
    { path: "/announcements", label: "Announcements", role: "support" },
    { path: "/scoreboard", label: "Scoreboard", role: "support" },
    { path: "/submissions", label: "Submissions", role: "support" },
    { path: "/pages", label: "Pages", role: "support" },
    { path: "/appearance", label: "Appearance", role: "admin" },
    { path: "/data", label: "Data", role: "admin" },
    { path: "/audit", label: "Audit log", role: "admin" },
    { path: "/settings", label: "Settings", role: "owner" },
  ];

  function active(path: string) {
    return path === "/" ? route.path === "/" : route.path === path || route.path.startsWith(path + "/");
  }

  const params = $derived.by(() => {
    for (const p of ["/challenges/:id", "/users/:id", "/teams/:id"]) {
      const m = match(p, route.path);
      if (m) return { pattern: p, id: m.id ?? "" };
    }
    return null;
  });
</script>

<div class="layout">
  <nav aria-label="Admin sections">
    <a class="brand" href={href("/")}>Flagfall <small>admin</small></a>
    <ul>
      {#each nav.filter((n) => can(n.role)) as n (n.path)}
        <li><a href={href(n.path)} aria-current={active(n.path) ? "page" : undefined}>{n.label}</a></li>
      {/each}
    </ul>
    <div class="who">
      <div>{session.me?.name}</div>
      <div class="muted"><span class="badge">{session.me?.role}</span></div>
      <div class="actions">
        <a href="/" target="_blank" rel="noopener">View event ↗</a>
        <button class="small" onclick={onSignOut}>Sign out</button>
      </div>
    </div>
  </nav>

  <main>
    {#if route.path === "/"}
      <Dashboard />
    {:else if route.path === "/event"}
      <EventPage />
    {:else if route.path === "/challenges"}
      <Challenges />
    {:else if params?.pattern === "/challenges/:id"}
      {#key params.id}<ChallengeEdit id={params.id} />{/key}
    {:else if route.path === "/users"}
      <Users />
    {:else if params?.pattern === "/users/:id"}
      {#key params.id}<UserEdit id={params.id} />{/key}
    {:else if route.path === "/teams"}
      <Teams />
    {:else if params?.pattern === "/teams/:id"}
      {#key params.id}<TeamEdit id={params.id} />{/key}
    {:else if route.path === "/announcements"}
      <Announcements />
    {:else if route.path === "/scoreboard"}
      <Scoreboard />
    {:else if route.path === "/submissions"}
      <Submissions />
    {:else if route.path === "/pages"}
      <Pages />
    {:else if route.path === "/appearance" && can("admin")}
      <Appearance />
    {:else if route.path === "/settings" && can("owner")}
      <Settings />
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

<style>
  .layout { display: grid; grid-template-columns: 13rem 1fr; min-height: 100vh; }
  nav { background: var(--panel); border-right: 1px solid var(--line); padding: 1rem 0.75rem; display: flex; flex-direction: column; position: sticky; top: 0; height: 100vh; }
  .brand { color: var(--text); font-weight: 600; font-size: 1.05rem; padding: 0 0.5rem 1rem; }
  .brand small { color: var(--muted); font-weight: 400; }
  ul { list-style: none; margin: 0; padding: 0; flex: 1; overflow-y: auto; }
  li a { display: block; padding: 0.35rem 0.5rem; border-radius: var(--radius); color: var(--muted); }
  li a:hover { color: var(--text); text-decoration: none; background: var(--panel-2); }
  li a[aria-current="page"] { color: var(--text); background: var(--panel-2); box-shadow: inset 2px 0 0 var(--accent); }
  .who { border-top: 1px solid var(--line); padding: 0.75rem 0.5rem 0; font-size: 0.9rem; }
  main { padding: 1.5rem 2rem 4rem; max-width: 72rem; min-width: 0; }
  @media (max-width: 760px) {
    .layout { grid-template-columns: 1fr; }
    nav { position: static; height: auto; }
    main { padding: 1rem; }
  }
</style>
