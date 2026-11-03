<script lang="ts">
  import { onMount } from "svelte";
  import { api, message, qs } from "../lib/api";
  import { href } from "../lib/router.svelte";
  import type { Paged, User } from "../lib/types";
  import { when } from "../lib/util";
  import Pager from "../components/Pager.svelte";
  import PeopleTabs from "../components/PeopleTabs.svelte";
  import UserEdit from "./UserEdit.svelte";

  // The list, with the selected user's editor beside it.
  let { selected = null }: { selected?: string | null } = $props();

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

<PeopleTabs />
{#if error}<p class="notice bad">{error}</p>{/if}

<div class="row">
  <section class="grow-main" aria-label="User list">
    <div class="toolbar">
      <input type="search" placeholder="Search name or email" aria-label="Search users" bind:value={search} oninput={onSearch} disabled={staffOnly} />
      <label class="check"><input type="checkbox" bind:checked={staffOnly} onchange={load} /><span>Staff only</span></label>
    </div>
    <div class="table-wrap">
      <table>
        <thead><tr><th scope="col">Name</th><th scope="col">Email</th><th scope="col">Team</th><th scope="col">Role</th><th scope="col">Status</th><th scope="col">Last seen</th></tr></thead>
        <tbody>
          {#each rows as u (u.id)}
            <tr class:sel={u.id === selected}>
              <td><a href={href(`/users/${u.id}`)} aria-current={u.id === selected ? "page" : undefined}>{u.name}</a></td>
              <td class="muted">{u.email}</td>
              <td>{#if u.teamId}<a href={href(`/teams/${u.teamId}`)}>{u.teamName}</a>{:else}<span class="muted">—</span>{/if}</td>
              <td>{#if u.role !== "player"}<span class="badge warn">{u.role}</span>{:else}<span class="muted">player</span>{/if}</td>
              <td><div class="badges">
                {#if u.banned}<span class="badge bad">Banned</span>{/if}
                {#if u.verified}<span class="badge ok">Verified</span>{/if}
              </div></td>
              <td class="muted small">{when(u.lastSeenAt)}</td>
            </tr>
          {:else}
            <tr><td colspan="6" class="empty">No users found.</td></tr>
          {/each}
        </tbody>
      </table>
    </div>
    {#if data && !staffOnly}
      <Pager page={data.page} pageSize={data.pageSize} total={data.total} onPage={(p) => { page = p; load(); }} />
    {/if}
  </section>

  {#if selected}
    <div class="side-panel stack">
      {#key selected}<UserEdit id={selected} onchange={load} />{/key}
    </div>
  {/if}
</div>
