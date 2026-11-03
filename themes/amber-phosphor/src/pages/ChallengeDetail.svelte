<script lang="ts">
  import { relativeTime, type Challenge, type FlagResult } from "@flagfall/theme-sdk";
  import { app, ff, refreshMe, t } from "../lib/state.svelte";
  import { cap, difficultyBars, errorMessage } from "../lib/util";
  import Icon from "../components/Icon.svelte";

  let { slug, onsolved }: { slug: string; onsolved: () => void } = $props();

  let challenge = $state<Challenge | null>(null);
  let notFound = $state(false);
  let flag = $state("");
  let busy = $state(false);
  let result = $state<{ text: string; ok: boolean } | null>(null);
  let confirmHint = $state<string | null>(null);

  async function load(s: string) {
    try {
      challenge = await ff.challenges.get(s);
      notFound = false;
    } catch {
      challenge = null;
      notFound = true;
    }
  }

  $effect(() => {
    void app.challengeVersion;
    load(slug);
  });
  $effect(() => {
    void slug;
    flag = "";
    result = null;
    confirmHint = null;
  });

  function describe(r: FlagResult): { text: string; ok: boolean } {
    switch (r.status) {
      case "correct":
        return { text: t(r.firstBlood ? "flag.correctFirstBlood" : "flag.correct", { points: r.points }), ok: true };
      case "incorrect":
        return { text: t("flag.incorrect"), ok: false };
      case "already_solved":
        return { text: t("flag.alreadySolved"), ok: true };
      case "rate_limited":
        return { text: t("flag.rateLimited", { seconds: r.retryAfterSeconds }), ok: false };
      case "event_not_running":
        return { text: t("flag.notRunning"), ok: false };
    }
  }

  async function submit(ev: SubmitEvent) {
    ev.preventDefault();
    if (!flag.trim() || busy) return;
    busy = true;
    try {
      const r = await ff.challenges.submitFlag(slug, flag);
      result = describe(r);
      if (r.status === "correct" || r.status === "already_solved") {
        flag = "";
        await Promise.all([load(slug), refreshMe()]);
        onsolved();
      }
    } catch (err) {
      result = { text: errorMessage(err, t), ok: false };
    } finally {
      busy = false;
    }
  }

  async function unlock(hintId: string) {
    if (confirmHint !== hintId) {
      confirmHint = hintId;
      return;
    }
    confirmHint = null;
    try {
      await ff.challenges.unlockHint(slug, hintId);
      await Promise.all([load(slug), refreshMe()]);
    } catch (err) {
      result = { text: errorMessage(err, t), ok: false };
    }
  }

  const solvedBy = $derived.by(() => {
    const team = app.me?.team;
    if (!team || !challenge?.solved) return null;
    return team.solves.find((s) => s.challengeSlug === challenge!.slug) ?? null;
  });
  const minutesIn = (iso: string) => {
    const start = app.event?.startsAt;
    if (!start) return "";
    const m = Math.max(0, Math.round((new Date(iso).getTime() - new Date(start).getTime()) / 60000));
    return m < 60 ? `${m}m in` : `${Math.floor(m / 60)}h ${String(m % 60).padStart(2, "0")}m in`;
  };
  const canPlay = $derived(!!app.me?.team && (app.event?.state === "running" || app.event?.state === "frozen"));
</script>

{#if notFound}
  <aside class="detail empty"><p>That challenge doesn't exist or isn't open yet.</p></aside>
{:else if !challenge}
  <aside class="detail empty"><p>Loading…</p></aside>
{:else}
  <aside class="detail" aria-labelledby="detail-title">
    <div class="detail-meta">
      <span class="mono">{challenge.category.toLowerCase()}</span>
      <span class="diff">{cap(challenge.difficulty)}
        <span class="bars" aria-hidden="true">{#each [1, 2, 3] as i}<span class="bar" class:on={i <= (difficultyBars[challenge.difficulty] ?? 2)}></span>{/each}</span>
      </span>
    </div>
    <h2 id="detail-title">{challenge.name}</h2>
    <div class="detail-stats">
      <span><b class="amber">{challenge.value}</b> {t("challenge.points")}</span>
      <span>{challenge.solveCount === 1 ? t("challenge.solve1") : t("challenge.solves", { n: challenge.solveCount })}</span>
      {#if challenge.author}<span>{t("challenge.by", { author: challenge.author })}</span>{/if}
    </div>

    <div class="desc">{@html challenge.descriptionHtml}</div>

    {#if challenge.connection}
      <div class="conn"><span class="muted">{t("challenge.connect")}</span><code>{challenge.connection}</code></div>
    {/if}
    {#if challenge.files.length}
      <div class="conn"><span class="muted">{t("challenge.files")}</span>
        <ul class="files">
          {#each challenge.files as f}<li><a href={f.url} download><Icon name="download" /> {f.name}</a> <span class="muted mono">({Math.ceil(f.size / 1024)} KB)</span></li>{/each}
        </ul>
      </div>
    {/if}

    <div class="kv"><span class="muted">{t("challenge.firstBlood")}</span>
      <span class="mono">{#if challenge.firstBlood}{challenge.firstBlood.teamName}, {minutesIn(challenge.firstBlood.solvedAt)}{:else}{t("challenge.firstBloodNone")}{/if}</span>
    </div>

    {#if challenge.solved}
      <div class="solved-box" role="status">
        <Icon name="check" size={22} />
        <div>
          <p class="strong">{t("challenge.solvedTitle")}</p>
          {#if solvedBy}<p>Solved by {solvedBy.userName} {relativeTime(solvedBy.solvedAt, t)}.</p>{/if}
        </div>
      </div>
    {:else if canPlay}
      <form class="flag-form" onsubmit={submit}>
        <label for="flag">{t("challenge.submit")}</label>
        <div class="flag-input"><span aria-hidden="true">$</span><input id="flag" name="flag" type="text" placeholder="flag&#123;...&#125;" autocomplete="off" spellcheck="false" bind:value={flag} /></div>
        {#if result}<p class="status-msg" class:ok={result.ok} class:bad={!result.ok} role="status">{result.text}</p>{/if}
        <button type="submit" class="btn" disabled={busy}>{t("challenge.submit")}</button>
      </form>
    {:else if !app.me}
      <a class="btn" href="/sign-in">Sign in to play</a>
    {/if}

    {#each challenge.hints as hint, i (hint.id)}
      {#if hint.content !== null}
        <div class="hint"><span class="muted">Hint {i + 1}</span>{@html hint.content}</div>
      {:else if canPlay && !challenge.solved}
        <button type="button" class="btn-ghost" onclick={() => unlock(hint.id)}>
          {confirmHint === hint.id ? t("challenge.confirmHint", { cost: hint.cost }) : t("challenge.unlockHint", { cost: hint.cost })}
        </button>
      {/if}
    {/each}
  </aside>
{/if}
