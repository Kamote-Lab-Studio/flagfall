<script lang="ts">
  import { api, message } from "../lib/api";
  import { can, notify } from "../lib/state.svelte";
  import type { ChallengeService, ServiceList, ServiceState } from "../lib/types";
  import { when } from "../lib/util";

  let { services, enabled, onchange }: { services: ChallengeService[]; enabled: boolean; onchange: () => void } = $props();

  let busy = $state("");

  const badge: Record<ServiceState, string> = { stopped: "", starting: "warn", running: "ok", unhealthy: "bad", failed: "bad" };

  async function act(s: ChallengeService, action: "start" | "stop" | "restart") {
    busy = s.slug;
    try {
      await api.post<ServiceList>(`/services/${encodeURIComponent(s.slug)}/${action}`);
      notify(action === "stop" ? `Stopping ${s.name}.` : `Starting ${s.name}. Building or pulling the image can take a minute.`);
      onchange();
    } catch (err) {
      notify(message(err), "error");
    } finally {
      busy = "";
    }
  }
</script>

<h2>Challenge containers</h2>
{#if !enabled}
  <p class="notice">
    Challenge containers are off. To let Flagfall start the {services.length === 1 ? "service" : `${services.length} services`} below, set
    <code>FLAGFALL_DOCKER_HOST</code> (see <code>docs/challenge-services.md</code>).
  </p>
{/if}
<table>
  <thead>
    <tr><th>Challenge</th><th>Image</th><th>Limits</th><th>Port</th><th>State</th>{#if enabled && can("admin")}<th><span class="sr-only">Actions</span></th>{/if}</tr>
  </thead>
  <tbody>
    {#each services as s (s.slug)}
      <tr>
        <td>{s.name}</td>
        <td><code>{s.image}</code></td>
        <td class="muted">{s.cpu} CPU · {s.memoryMB} MB</td>
        <td>
          {#if s.hostPort}<code>{s.hostPort}</code> <span class="muted">→ {s.port}</span>{:else}<span class="muted">{s.port}</span>{/if}
        </td>
        <td>
          <span class="badge {badge[s.state] ?? ''}">{s.state}</span>
          {#if s.message}<div class="muted small">{s.message}</div>{/if}
          {#if s.checkedAt}<div class="muted small">checked {when(s.checkedAt)}</div>{/if}
        </td>
        {#if enabled && can("admin")}
          <td><div class="actions">
            {#if s.desired === "running"}
              <button type="button" class="small" disabled={busy === s.slug} onclick={() => act(s, "restart")}>Restart</button>
              <button type="button" class="small danger" disabled={busy === s.slug} onclick={() => act(s, "stop")}>Stop</button>
            {:else}
              <button type="button" class="small" disabled={busy === s.slug} onclick={() => act(s, "start")}>Start</button>
            {/if}
          </div></td>
        {/if}
      </tr>
    {/each}
  </tbody>
</table>
<p class="hint">One shared container per challenge, on its own network. Players reach it on the published port of this host.</p>
