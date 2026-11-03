<script lang="ts">
  import { onMount } from "svelte";
  import { api, ApiError, fieldErrors, message, qs } from "../lib/api";
  import { can, notify } from "../lib/state.svelte";
  import type { Event } from "../lib/types";

  const datasets = [
    { id: "scoreboard", label: "Scoreboard (unfrozen, hidden teams unranked)" },
    { id: "teams", label: "Teams" },
    { id: "users", label: "Users (with emails)" },
    { id: "solves", label: "Solves" },
    { id: "submissions", label: "Submissions (correct flags hidden)" },
  ];
  let dataset = $state("scoreboard");
  let eventName = $state("");

  let restoreFile = $state<File | null>(null);
  let restoreConfirm = $state("");
  let restoreErrors = $state<Record<string, string>>({});
  let restoring = $state(false);

  let resetConfirm = $state("");
  let resetErrors = $state<Record<string, string>>({});
  let resetting = $state(false);

  const exportURL = (format: string) => "/admin/api/v1/export" + qs({ dataset, format });

  async function restore(ev: SubmitEvent) {
    ev.preventDefault();
    if (!restoreFile) return;
    if (!confirm("Replace EVERYTHING in this install with the backup? Everyone, you included, will be signed out.")) return;
    restoring = true;
    restoreErrors = {};
    const fd = new FormData();
    fd.append("confirm", restoreConfirm);
    fd.append("file", restoreFile);
    try {
      const res = await fetch("/admin/api/v1/restore", { method: "POST", body: fd, credentials: "same-origin" });
      const body = await res.json().catch(() => null);
      if (!res.ok) throw new ApiError(res.status, body?.error?.code ?? "error", body?.error?.message ?? `Restore failed (${res.status}).`, body?.error?.fields ?? {});
      notify("Backup restored. Sign in again.");
      setTimeout(() => location.assign("/admin"), 1500);
    } catch (err) {
      restoreErrors = fieldErrors(err);
      if (!Object.keys(restoreErrors).length) notify(message(err), "error");
    } finally {
      restoring = false;
    }
  }

  async function reset(ev: SubmitEvent) {
    ev.preventDefault();
    if (!confirm("Delete all players, teams, submissions and solves? Challenges, pages and staff stay.")) return;
    resetting = true;
    resetErrors = {};
    try {
      const r = await api.post<Record<string, number>>("/reset", { confirm: resetConfirm });
      notify(`Event reset: ${r.teams ?? 0} teams and ${r.players ?? 0} players removed.`);
      resetConfirm = "";
    } catch (err) {
      resetErrors = fieldErrors(err);
      if (!Object.keys(resetErrors).length) notify(message(err), "error");
    } finally {
      resetting = false;
    }
  }

  onMount(async () => {
    eventName = (await api.get<Event>("/event")).name;
  });
</script>

<h1>Data</h1>

<h2>Export</h2>
<div class="card">
  <p class="muted">For results, prizes and records. Exports are read-only copies; they can't be restored.</p>
  <div class="toolbar" style="margin:0">
    <select bind:value={dataset} aria-label="What to export">
      {#each datasets as d (d.id)}<option value={d.id}>{d.label}</option>{/each}
    </select>
    <a class="button" href={exportURL("csv")} download>Download CSV</a>
    <a class="button" href={exportURL("json")} download>Download JSON</a>
  </div>
</div>

{#if can("owner")}
  <h2>Backup</h2>
  <div class="card">
    <p class="muted">
      One <code>.zip</code> with the whole database and every uploaded file. It contains password hashes and flag hashes,
      so keep it somewhere safe. Take one before the event starts, and before any restore or reset.
    </p>
    <a class="button primary-link" href="/admin/api/v1/backup" download>Download backup</a>
    <p class="hint" style="margin-top:0.75rem">From the server: <code>flagfall backup flagfall.zip</code> (works with cron).</p>
  </div>

  <h2>Restore</h2>
  <form class="card danger-zone" onsubmit={restore}>
    <p>Replaces <b>everything</b> in this install with a backup. Everyone is signed out.</p>
    <label><span>Backup file</span>
      <input type="file" accept=".zip,application/zip" onchange={(e) => (restoreFile = e.currentTarget.files?.[0] ?? null)} required />
      {#if restoreErrors.file}<div class="field-error">{restoreErrors.file}</div>{/if}</label>
    <label><span>Type the current event's name, <b>{eventName}</b>, to confirm</span>
      <input bind:value={restoreConfirm} autocomplete="off" required />
      {#if restoreErrors.confirm}<div class="field-error">{restoreErrors.confirm}</div>{/if}</label>
    <button class="danger" disabled={restoring || !restoreFile || restoreConfirm !== eventName}>{restoring ? "Restoring…" : "Restore backup"}</button>
  </form>

  <h2>Reset the event</h2>
  <form class="card danger-zone" onsubmit={reset}>
    <p>
      Deletes all players, teams, submissions, solves, hint unlocks and score adjustments. Keeps challenges, pages,
      announcements, brackets, settings, staff accounts and the audit log. Use it after a test run.
    </p>
    <label><span>Type the event's name, <b>{eventName}</b>, to confirm</span>
      <input bind:value={resetConfirm} autocomplete="off" required />
      {#if resetErrors.confirm}<div class="field-error">{resetErrors.confirm}</div>{/if}</label>
    <button class="danger" disabled={resetting || resetConfirm !== eventName}>{resetting ? "Resetting…" : "Reset the event"}</button>
  </form>
{/if}

<style>
  .danger-zone { border-color: color-mix(in srgb, var(--bad) 45%, var(--line)); }
  .primary-link { background: var(--accent); border-color: var(--accent); color: var(--accent-ink); }
</style>
