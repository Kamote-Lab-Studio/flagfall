<script lang="ts">
  import { href, route } from "../lib/router.svelte";

  // "Users and teams" is one section in the design, with a tab per list.
  const onTeams = $derived(route.path === "/teams" || route.path.startsWith("/teams/"));
</script>

<div class="page-head">
  <div>
    <h1>Users and teams</h1>
    <p class="muted">Open a team or player to edit it. Changes are recorded in the audit log.</p>
  </div>
  <nav class="tabs" aria-label="Lists">
    <a href={href("/teams")} aria-current={onTeams ? "page" : undefined}>Teams</a>
    <a href={href("/users")} aria-current={onTeams ? undefined : "page"}>Users</a>
  </nav>
</div>
