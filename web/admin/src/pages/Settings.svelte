<script lang="ts">
  import { onMount } from "svelte";
  import { api, fieldErrors, message, qs } from "../lib/api";
  import { href } from "../lib/router.svelte";
  import { notify, session } from "../lib/state.svelte";
  import { roles, type Paged, type Role, type User } from "../lib/types";

  interface Settings {
    smtpHost: string; smtpPort: number; smtpUser: string; smtpPasswordSet: boolean; smtpFrom: string;
    smtpSecurity: string; smtpProblem: string | null; storage: { kind: string; path: string };
  }

  let s = $state<Settings | null>(null);
  let form = $state({ smtpHost: "", smtpPort: 587, smtpUser: "", smtpPassword: "", smtpFrom: "", smtpSecurity: "starttls" });
  let changePassword = $state(false);
  let errors = $state<Record<string, string>>({});
  let busy = $state(false);
  let testing = $state(false);

  let staff = $state<User[]>([]);
  let search = $state("");
  let found = $state<User[]>([]);

  function fill(x: Settings) {
    s = x;
    form = { smtpHost: x.smtpHost, smtpPort: x.smtpPort, smtpUser: x.smtpUser, smtpPassword: "", smtpFrom: x.smtpFrom, smtpSecurity: x.smtpSecurity };
    changePassword = !x.smtpPasswordSet;
  }

  async function load() {
    try {
      fill(await api.get<Settings>("/settings"));
      staff = await api.get<User[]>("/staff");
    } catch (err) {
      notify(message(err), "error");
    }
  }

  async function save(ev: SubmitEvent) {
    ev.preventDefault();
    busy = true;
    errors = {};
    const body: Record<string, unknown> = { ...form };
    if (!changePassword) delete body.smtpPassword;
    try {
      fill(await api.patch<Settings>("/settings", body));
      notify(form.smtpHost ? "Mail settings saved." : "Email turned off.");
    } catch (err) {
      errors = fieldErrors(err);
      notify(message(err), "error");
    } finally {
      busy = false;
    }
  }

  async function sendTest() {
    testing = true;
    try {
      const r = await api.post<{ sentTo: string }>("/settings/test-email");
      notify(`Test email sent to ${r.sentTo}.`);
    } catch (err) {
      notify(message(err), "error");
    } finally {
      testing = false;
    }
  }

  function securityChanged() {
    if (form.smtpSecurity === "tls" && form.smtpPort === 587) form.smtpPort = 465;
    if (form.smtpSecurity === "starttls" && form.smtpPort === 465) form.smtpPort = 587;
  }

  async function setRole(u: User, role: Role) {
    if (u.id === session.me?.id && !confirm("Change your own role? You may lose access to this page.")) {
      staff = [...staff];
      return;
    }
    try {
      await api.patch(`/users/${u.id}`, { role });
      notify(`${u.name} is now ${role === "player" ? "a player" : `${role}`}.`);
      staff = await api.get<User[]>("/staff");
      search = "";
      found = [];
    } catch (err) {
      notify(message(err), "error");
      staff = await api.get<User[]>("/staff");
    }
  }

  async function find() {
    found = search ? (await api.get<Paged<User>>("/users" + qs({ search }))).items.filter((u) => u.role === "player") : [];
  }

  onMount(load);
</script>

<h1>Settings</h1>

{#if s}
  <h2>Email</h2>
  <form class="card" onsubmit={save}>
    <p class="muted">Used for password reset emails. Without it, staff can still make reset links on a user's page.</p>
    {#if s.smtpProblem}<div class="notice bad">{s.smtpProblem}</div>{/if}
    <div class="grid">
      <label><span>Mail server (empty = email off)</span><input bind:value={form.smtpHost} placeholder="smtp.example.org" autocomplete="off" />
        {#if errors.smtpHost}<div class="field-error">{errors.smtpHost}</div>{/if}</label>
      <label><span>Security</span>
        <select bind:value={form.smtpSecurity} onchange={securityChanged}>
          <option value="starttls">STARTTLS (port 587)</option>
          <option value="tls">TLS (port 465)</option>
          <option value="none">None (local relay only)</option>
        </select>
        {#if errors.smtpSecurity}<div class="field-error">{errors.smtpSecurity}</div>{/if}</label>
      <label><span>Port</span><input type="number" min="1" max="65535" bind:value={form.smtpPort} />
        {#if errors.smtpPort}<div class="field-error">{errors.smtpPort}</div>{/if}</label>
      <label><span>From</span><input bind:value={form.smtpFrom} placeholder="Flagfall CTF <ctf@example.org>" />
        {#if errors.smtpFrom}<div class="field-error">{errors.smtpFrom}</div>{/if}</label>
      <label><span>User name</span><input bind:value={form.smtpUser} autocomplete="off" />
        {#if errors.smtpUser}<div class="field-error">{errors.smtpUser}</div>{/if}</label>
      <label><span>Password</span>
        {#if changePassword}
          <input type="password" bind:value={form.smtpPassword} autocomplete="new-password" />
        {:else}
          <div class="actions" style="margin:0"><span class="muted">Saved (hidden)</span><button type="button" class="small" onclick={() => (changePassword = true)}>Change</button></div>
        {/if}
        {#if errors.smtpPassword}<div class="field-error">{errors.smtpPassword}</div>{/if}</label>
    </div>
    {#if form.smtpSecurity === "none"}<p class="hint">With no security, the password is only sent to a server on this machine.</p>{/if}
    <div class="actions">
      <button class="primary" disabled={busy}>Save</button>
      <button type="button" onclick={sendTest} disabled={testing || !s.smtpHost}>{testing ? "Sending…" : `Send a test email to ${session.me?.email}`}</button>
    </div>
  </form>

  <h2>File storage</h2>
  <div class="card">
    <p>Challenge files are kept on this server's disk at <code>{s.storage.path}</code>, and included in backups.</p>
    <p class="hint">Storage is set when the server starts, not here, because changing it during an event would break every download.</p>
  </div>

  <h2>Staff</h2>
  <table>
    <thead><tr><th>Name</th><th>Email</th><th>Role</th></tr></thead>
    <tbody>
      {#each staff as u (u.id)}
        <tr>
          <td><a href={href(`/users/${u.id}`)}>{u.name}</a>{#if u.id === session.me?.id} <span class="muted">(you)</span>{/if}</td>
          <td>{u.email}</td>
          <td>
            <select aria-label={`Role for ${u.name}`} value={u.role} onchange={(e) => setRole(u, e.currentTarget.value as Role)}>
              {#each roles as r (r)}<option value={r}>{r === "player" ? "player (remove from staff)" : r}</option>{/each}
            </select>
          </td>
        </tr>
      {/each}
    </tbody>
  </table>
  <div class="toolbar" style="margin-top:0.75rem">
    <input type="search" style="min-width:18rem" placeholder="Find a player to make staff" aria-label="Find a player to make staff" bind:value={search} oninput={find} />
    {#each found.slice(0, 6) as u (u.id)}
      <button class="small" onclick={() => setRole(u, "support")}>+ {u.name}</button>
    {/each}
  </div>
  <p class="hint">
    New staff start as <b>support</b>. Support: users, teams, announcements. Author: also challenges. Admin: everything
    except staff, settings and restores. Owner: everything. There is always at least one owner.
  </p>
{/if}
