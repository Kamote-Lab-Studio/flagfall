<script lang="ts">
  import { onMount } from "svelte";
  import { api } from "../lib/api";
  import { href } from "../lib/router.svelte";
  import type { Event } from "../lib/types";

  // The event's name and state with a live countdown, as in the design's top bar.
  let ev = $state<Event | null>(null);
  let now = $state(Date.now());

  async function load() {
    try {
      ev = await api.get<Event>("/event");
    } catch {
      // The bar is a convenience; pages show their own errors.
    }
  }

  onMount(() => {
    load();
    const tick = setInterval(() => (now = Date.now()), 1000);
    const refresh = setInterval(load, 60_000);
    return () => {
      clearInterval(tick);
      clearInterval(refresh);
    };
  });

  const ms = (iso: string | null) => (iso ? new Date(iso).getTime() - now : null);

  /** 14:22:07 under a day, "3d 4h" above. */
  function clock(t: number): string {
    const s = Math.max(0, Math.floor(t / 1000));
    const d = Math.floor(s / 86400);
    const h = Math.floor((s % 86400) / 3600);
    const m = Math.floor((s % 3600) / 60);
    const pad = (n: number) => String(n).padStart(2, "0");
    return d > 0 ? `${d}d ${h}h ${pad(m)}m` : `${pad(h)}:${pad(m)}:${pad(s % 60)}`;
  }

  function span(t: number): string {
    const m = Math.max(0, Math.round(t / 60000));
    const d = Math.floor(m / 1440);
    const h = Math.floor((m % 1440) / 60);
    if (d > 0) return `${d}d ${h}h`;
    return h > 0 ? `${h}h ${m % 60}m` : `${m % 60}m`;
  }

  const toStart = $derived(ev ? ms(ev.startsAt) : null);
  const toEnd = $derived(ev ? ms(ev.endsAt) : null);
  const toFreeze = $derived(ev ? ms(ev.freezeAt) : null);
  const phase = $derived.by(() => {
    if (!ev) return "";
    if (toStart !== null && toStart > 0) return "upcoming";
    if (toEnd !== null && toEnd <= 0) return "over";
    return "live";
  });
</script>

<header class="adm-top">
  <div class="adm-status">
    {#if ev}
      <b>{ev.name}</b>
      {#if phase === "live"}
        <span class="live"><span aria-hidden="true"></span>Live</span>
        {#if toEnd !== null}
          <span class="muted"><span class="mono white">{clock(toEnd)}</span> left{#if toFreeze !== null && toFreeze > 0}, scoreboard freezes in {span(toFreeze)}{:else if toFreeze !== null}, scoreboard frozen{/if}</span>
        {/if}
      {:else if phase === "upcoming"}
        <span class="badge warn">Not started</span>
        <span class="muted">starts in <span class="mono white">{clock(toStart ?? 0)}</span></span>
      {:else}
        <span class="badge">Over</span>
      {/if}
    {/if}
  </div>
  <div class="adm-actions">
    <a class="btn-line" href={href("/announcements")}>Post announcement</a>
    <!-- The freeze is the event's freezeAt time, set on the Event page. -->
    <a class="btn-line" href={href("/event")}>{toFreeze !== null && toFreeze <= 0 ? "Scoreboard frozen" : "Freeze scoreboard"}</a>
  </div>
</header>

<style>
  .adm-top { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 12px 24px; padding: 14px 32px; border-bottom: 1px solid var(--line); }
  .adm-status { display: flex; flex-wrap: wrap; align-items: center; gap: 8px 14px; font-size: 14px; min-height: 40px; }
  .adm-status b { font-size: 15px; }
  .live { display: inline-flex; align-items: center; gap: 6px; padding: 2px 10px; border-radius: 10px; background: var(--ok-bg); color: var(--ok); font-size: 13px; font-weight: 600; }
  .live span { width: 8px; height: 8px; border-radius: 50%; background: var(--ok); }
  @media (max-width: 700px) { .adm-top { padding: 12px 20px; } }
</style>
