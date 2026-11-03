<script lang="ts">
  import { isError, type ChallengeSummary } from "@flagfall/theme-sdk";
  import { app, ff, setting, t } from "../lib/state.svelte";
  import { navigate } from "../lib/router.svelte";
  import { cap } from "../lib/util";
  import Icon from "../components/Icon.svelte";
  import ChallengeDetail from "./ChallengeDetail.svelte";

  let { params }: { params: { slug?: string } } = $props();

  let challenges = $state<ChallengeSummary[] | null>(null);
  let category = $state("all");
  let error = $state("");

  async function load() {
    try {
      challenges = await ff.challenges.list();
    } catch (err) {
      if (isError(err, "unauthenticated")) navigate("/sign-in?next=" + encodeURIComponent(location.pathname), { replace: true });
      else error = (err as Error).message;
    }
  }

  $effect(() => {
    void app.challengeVersion;
    void app.scoreVersion;
    void app.me?.team?.score;
    load();
  });

  const categories = $derived.by(() => {
    const counts = new Map<string, number>();
    for (const c of challenges ?? []) counts.set(c.category, (counts.get(c.category) ?? 0) + 1);
    return [...counts];
  });
  const visible = $derived((challenges ?? []).filter((c) => category === "all" || c.category === category));
  const solved = $derived((challenges ?? []).filter((c) => c.solved).length);
  const total = $derived(challenges?.length ?? 0);
  const listLayout = $derived(setting("boardLayout") === "list");
</script>

<main class="board">
  <section class="board-main" aria-labelledby="board-title">
    <div class="page-head">
      <div><h1 id="board-title">{t("board.title")}</h1><p class="muted">{setting("boardTagline")}</p></div>
      {#if app.me?.team && total > 0}
        <div class="progress">
          <div class="progress-row"><span class="muted">{t("board.progress")}</span><span>{t("board.solvedOf", { solved, total })}</span></div>
          <div class="progress-bar"><span style:width="{Math.round((solved / total) * 100)}%"></span></div>
        </div>
      {/if}
    </div>

    {#if app.me && !app.me.team}
      <div class="banner"><span>{t("board.noTeam")}</span><a href="/account">{t("board.noTeamLink")}</a></div>
    {/if}

    {#if error}
      <p class="error" role="alert">{error}</p>
    {:else if !challenges}
      <p class="loading">Loading challenges…</p>
    {:else if challenges.length === 0}
      <p class="muted">No challenges are open yet. Check back soon.</p>
    {:else}
      <div class="filters" role="group" aria-label="Filter by category">
        <button type="button" aria-pressed={category === "all"} onclick={() => (category = "all")}>{t("board.all")} <span class="mono">{total}</span></button>
        {#each categories as [name, count]}
          <button type="button" aria-pressed={category === name} onclick={() => (category = name)}>{name} <span class="mono">{count}</span></button>
        {/each}
      </div>

      <div class="tiles" class:list={listLayout}>
        {#each visible as c (c.slug)}
          <a
            class="tile c-{c.category.toLowerCase()}"
            class:solved={c.solved}
            class:selected={c.slug === params.slug}
            href="/challenges/{c.slug}"
            aria-current={c.slug === params.slug ? "true" : undefined}
          >
            <span class="tile-top"><span class="mono muted">{c.category.toLowerCase()}</span><span class="muted">{cap(c.difficulty)}</span></span>
            <span class="tile-name">{c.name}</span>
            <span class="tile-bottom">
              <span class="tile-pts">{c.value}</span>
              {#if c.solved}
                <span class="tile-done"><Icon name="check" />Solved</span>
              {:else}
                <span class="muted">{c.solveCount === 1 ? t("challenge.solve1") : t("challenge.solves", { n: c.solveCount })}</span>
              {/if}
            </span>
          </a>
        {/each}
      </div>
    {/if}
  </section>

  {#if params.slug}
    <ChallengeDetail slug={params.slug} onsolved={load} />
  {:else if challenges && challenges.length > 0}
    <aside class="detail empty"><p>{t("board.pick")}</p></aside>
  {/if}
</main>
