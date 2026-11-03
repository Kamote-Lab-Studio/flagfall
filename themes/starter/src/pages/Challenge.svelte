<script lang="ts">
  import type { Challenge } from "@flagfall/theme-sdk";
  import { app, errorText, ff, refreshMe } from "../lib/app.svelte";

  let { params }: { params: Record<string, string> } = $props();

  let c = $state<Challenge | null>(null);
  let error = $state("");
  let flag = $state("");
  let result = $state<{ text: string; good: boolean } | null>(null);
  let busy = $state(false);

  $effect(() => {
    app.version.challenges;
    ff.challenges.get(params.slug ?? "").then((x) => (c = x), (err) => (error = errorText(err)));
  });

  async function submit(ev: SubmitEvent) {
    ev.preventDefault();
    if (!c) return;
    busy = true;
    try {
      // submitFlag never throws for wrong or rate-limited flags: check status.
      const r = await ff.challenges.submitFlag(c.slug, flag);
      if (r.status === "correct") {
        result = { text: `Correct! +${r.points} points.${r.firstBlood ? " First blood!" : ""}`, good: true };
        flag = "";
        refreshMe();
        app.version.challenges++;
      } else if (r.status === "incorrect") result = { text: "Not quite. Try again.", good: false };
      else if (r.status === "already_solved") result = { text: "Your team already solved this.", good: true };
      else if (r.status === "rate_limited") result = { text: `Too many tries. Wait ${r.retryAfterSeconds} seconds.`, good: false };
      else result = { text: "The event isn't running, so flags can't be submitted.", good: false };
    } catch (err) {
      result = { text: errorText(err), good: false };
    } finally {
      busy = false;
    }
  }

  async function unlock(hintId: string, cost: number) {
    if (!c || !confirm(`Unlock this hint for ${cost} points?`)) return;
    try {
      await ff.challenges.unlockHint(c.slug, hintId);
      c = await ff.challenges.get(c.slug);
    } catch (err) {
      error = errorText(err);
    }
  }
</script>

<p><a href="/">← All challenges</a></p>

{#if !c}
  <h1>Challenge</h1>
  {#if !app.me && error}
    <!-- Events can keep challenges private until players sign in. -->
    <p><a href="/sign-in?next=/challenges/{params.slug}">Sign in</a> to see this challenge.</p>
  {:else if error}
    <p class="error" role="alert">{error}</p>
  {:else}
    <p>Loading…</p>
  {/if}
{:else}
  <h1>{c.name}</h1>
  <p class="muted">{c.category} · {c.difficulty} · {c.value} points · by {c.author || "unknown"}</p>
  <!-- descriptionHtml is sanitized by the server. -->
  <div class="description">{@html c.descriptionHtml}</div>

  {#if c.connection}<p>Connect: <code>{c.connection}</code></p>{/if}

  {#if c.files.length}
    <h2>Files</h2>
    <ul>{#each c.files as f (f.url)}<li><a href={f.url} download>{f.name}</a> <span class="muted">({f.size} bytes)</span></li>{/each}</ul>
  {/if}

  {#if c.hints.length}
    <h2>Hints</h2>
    <ul>
      {#each c.hints as h (h.id)}
        <li>
          {#if h.content !== null}{h.content}{:else}<button type="button" class="secondary" onclick={() => unlock(h.id, h.cost)}>Unlock for {h.cost} points</button>{/if}
        </li>
      {/each}
    </ul>
  {/if}

  <h2>Submit a flag</h2>
  {#if c.solved}
    <p class="ok">Solved by your team.</p>
  {:else if !app.me}
    <p><a href="/sign-in">Sign in</a> to submit flags.</p>
  {:else if !app.me.team}
    <p><a href="/account">Join a team</a> to submit flags.</p>
  {:else}
    <form onsubmit={submit}>
      <label>Flag <input bind:value={flag} required autocomplete="off" spellcheck="false" /></label>
      <button disabled={busy}>Submit</button>
      {#if result}<p class={result.good ? "ok" : "error"} role="status">{result.text}</p>{/if}
    </form>
  {/if}

  <p class="muted">
    {#if c.firstBlood}First blood: <a href="/teams/{c.firstBlood.teamId}">{c.firstBlood.teamName}</a>{:else}Not solved yet.{/if}
  </p>
{/if}
