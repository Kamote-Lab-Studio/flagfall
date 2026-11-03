<script lang="ts">
  import { onMount } from "svelte";
  import { api, fieldErrors, message } from "../lib/api";
  import { href } from "../lib/router.svelte";
  import { can, notify } from "../lib/state.svelte";
  import { roles, type User } from "../lib/types";
  import { when } from "../lib/util";

  // Shown beside the user list; onchange lets the list refresh.
  let { id, onchange }: { id: string; onchange?: () => void } = $props();

  let u = $state<User | null>(null);
  let form = $state({ name: "", email: "", country: "", verified: false, banned: false, role: "player" as User["role"] });
  let errors = $state<Record<string, string>>({});
  let error = $state("");
  let resetLink = $state("");
  let busy = $state(false);

  function fill(x: User) {
    u = x;
    form = { name: x.name, email: x.email, country: x.country ?? "", verified: x.verified, banned: x.banned, role: x.role };
  }

  async function load() {
    try {
      fill(await api.get<User>(`/users/${id}`));
    } catch (err) {
      error = message(err);
    }
  }

  async function save(ev: SubmitEvent) {
    ev.preventDefault();
    if (!u) return;
    if (form.banned && !u.banned && !confirm(`Ban ${u.name}? They'll be signed out everywhere.`)) return;
    busy = true;
    errors = {};
    const body: Record<string, unknown> = {
      name: form.name, email: form.email, country: form.country || null, verified: form.verified, banned: form.banned,
    };
    if (can("owner") && form.role !== u.role) body.role = form.role;
    try {
      fill(await api.patch<User>(`/users/${id}`, body));
      notify("User saved.");
      onchange?.();
    } catch (err) {
      errors = fieldErrors(err);
      notify(message(err), "error");
    } finally {
      busy = false;
    }
  }

  async function reset() {
    try {
      const r = await api.post<{ emailed: boolean; link?: string }>(`/users/${id}/password-reset`);
      if (r.link) resetLink = r.link;
      else notify("Reset link emailed.");
    } catch (err) {
      notify(message(err), "error");
    }
  }

  onMount(load);
</script>

{#if error}<p class="notice bad">{error}</p>{/if}
{#if u}
  <form class="panel pad" onsubmit={save} aria-labelledby="user-title">
    <div>
      <div class="panel-head">
        <h2 id="user-title">{u.name}</h2>
        <a class="small link" href={href("/users")} aria-label="Close the editor">Close</a>
      </div>
      <p class="muted small">
        Joined {when(u.createdAt)}, last seen {when(u.lastSeenAt)}.
        {#if u.teamId}Team <a href={href(`/teams/${u.teamId}`)}>{u.teamName}</a>.{:else}No team.{/if}
      </p>
      {#if u.role !== "player" || u.banned}<div class="badges" style="margin-top:6px">
        {#if u.role !== "player"}<span class="badge warn">{u.role}</span>{/if}
        {#if u.banned}<span class="badge bad">Banned</span>{/if}
      </div>{/if}
    </div>
    <div class="grid2">
      <label><span>Name</span><input bind:value={form.name} required />
        {#if errors.name}<div class="field-error">{errors.name}</div>{/if}</label>
      <label><span>Email</span><input type="email" bind:value={form.email} required />
        {#if errors.email}<div class="field-error">{errors.email}</div>{/if}</label>
      <label><span>Country (ISO code)</span><input bind:value={form.country} maxlength="2" />
        {#if errors.country}<div class="field-error">{errors.country}</div>{/if}</label>
      <label><span>Role</span>
        <select bind:value={form.role} disabled={!can("owner")}>
          {#each roles as r (r)}<option value={r}>{r}</option>{/each}
        </select>
        {#if !can("owner")}<div class="hint">Only owners change roles.</div>{/if}
        {#if errors.role}<div class="field-error">{errors.role}</div>{/if}</label>
    </div>
    <label class="check"><input type="checkbox" bind:checked={form.verified} /><span>Verified (can join verified-only brackets)</span></label>
    <label class="check"><input type="checkbox" bind:checked={form.banned} /><span>Banned</span></label>
    <div class="actions">
      <button class="primary grow" disabled={busy}>Save user</button>
      <button type="button" onclick={reset}>Send password reset</button>
    </div>
    {#if resetLink}
      <div class="notice stack-s">
        <p>No email server is set up, so pass this link on yourself. It works for one hour.</p>
        <input class="mono" readonly value={resetLink} onfocus={(e) => e.currentTarget.select()} />
      </div>
    {/if}
  </form>
{/if}
