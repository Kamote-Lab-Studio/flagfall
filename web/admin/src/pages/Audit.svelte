<script lang="ts">
  import { onMount } from "svelte";
  import { api, message, qs } from "../lib/api";
  import type { AuditEntry } from "../lib/types";
  import { when } from "../lib/util";

  const PAGE = 100;
  let items = $state<AuditEntry[]>([]);
  let more = $state(false);
  let error = $state("");

  async function fetchPage(before?: string) {
    const page = await api.get<AuditEntry[]>("/audit" + qs({ before, limit: PAGE }));
    more = page.length === PAGE;
    return page;
  }

  async function load() {
    try {
      items = await fetchPage();
    } catch (err) {
      error = message(err);
    }
  }

  async function older() {
    const last = items.at(-1);
    if (last) items = [...items, ...(await fetchPage(last.id))];
  }

  function pretty(s: string | null): string {
    if (!s) return "";
    try {
      return JSON.stringify(JSON.parse(s));
    } catch {
      return s;
    }
  }

  onMount(load);
</script>

<h1>Audit log</h1>
<p class="muted">Every change made through the admin panel. Passwords and flags are removed before saving.</p>
{#if error}<p class="notice bad">{error}</p>{/if}
<table>
  <thead><tr><th>When</th><th>Who</th><th>Action</th><th>Target</th><th>Details</th><th>IP</th></tr></thead>
  <tbody>
    {#each items as a (a.id)}
      <tr>
        <td class="muted nowrap">{when(a.at)}</td>
        <td>{a.actorName ?? "—"}</td>
        <td class="mono nowrap">{a.action}</td>
        <td class="mono">{pretty(a.target)}</td>
        <td class="mono details" title={pretty(a.details)}>{pretty(a.details)}</td>
        <td class="mono muted">{a.ip ?? "—"}</td>
      </tr>
    {:else}
      <tr><td colspan="6" class="empty">Nothing yet.</td></tr>
    {/each}
  </tbody>
</table>
{#if more}<div class="actions"><button onclick={older}>Older</button></div>{/if}

<style>
  .nowrap { white-space: nowrap; }
  .details { max-width: 24rem; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 0.8rem; }
</style>
