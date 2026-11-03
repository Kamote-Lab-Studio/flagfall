<script lang="ts">
  import { onMount } from "svelte";
  import { api, message, qs } from "../lib/api";
  import { href } from "../lib/router.svelte";
  import type { Paged, User } from "../lib/types";
  import { when } from "../lib/util";
  import Pager from "../components/Pager.svelte";

  let data = $state<Paged<User> | null>(null);
  let search = $state("");
  let page = $state(1);
  let error = $state("");
  let staffOnly = $state(false);
  let staff = $state<User[]>([]);

  async function load() {
    try {
      if (staffOnly) staff = await api.get<User[]>("/staff");
      else data = await api.get<Paged<User>>("/users" + qs({ search, page }));
      error = "";
    } catch (err) {
      error = message(err);
    }
  }

  let timer: ReturnType<typeof setTimeout> | undefined;
  function onSearch() {
    clearTimeout(timer);
    timer = setTimeout(() => {
      page = 1;
      load();
    }, 250);
  }

  const rows = $derived(staffOnly ? staff : (data?.items ?? []));

  onMount(load);
</script>

<h1>Users</h1>
{#if error}<p class="notice bad">{error}</p>{/if}
<div class="toolbar">
  <input type="search" placeholder="Search name or email" aria-label="Search users" bind:value={search} oninput={onSearch} disabled={staffOnly} />
  <label class="check" style="margin:0"><input type="checkbox" bind:checked={staffOnly} onchange={load} /><span>Staff only</span></label>
</div>

<table>
  <thead><tr><th>Name</th><th>Email</th><th>Team</th><th>Role</th><th>Status</th><th>Last seen</th></tr></thead>
  <tbody>
    {#each rows as u (u.id)}
      <tr>
        <td><a href={href(`/users/${u.id}`)}>{u.name}</a></td>
        <td>{u.email}</td>
        <td>{#if u.teamId}<a href={href(`/teams/${u.teamId}`)}>{u.teamName}</a>{:else}<span class="muted">—</span>{/if}</td>
        <td>{#if u.role !== "player"}<span class="badge">{u.role}</span>{:else}<span class="muted">player</span>{/if}</td>
        <td>
          {#if u.banned}<span class="badge bad">banned</span>{/if}
          {#if u.verified}<span class="badge ok">verified</span>{/if}
        </td>
        <td class="muted">{when(u.lastSeenAt)}</td>
      </tr>
    {:else}
      <tr><td colspan="6" class="empty">No users found.</td></tr>
    {/each}
  </tbody>
</table>
{#if data && !staffOnly}
  <Pager page={data.page} pageSize={data.pageSize} total={data.total} onPage={(p) => { page = p; load(); }} />
{/if}
