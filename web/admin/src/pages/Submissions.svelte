<script lang="ts">
  import { onMount } from "svelte";
  import { api, message, qs } from "../lib/api";
  import { href, route } from "../lib/router.svelte";
  import { can } from "../lib/state.svelte";
  import type { FairPlayAlert, Submission, SubmissionSummary } from "../lib/types";
  import { ago, when } from "../lib/util";

  // Laid out after design/option-a/admin/submissions.html: result pills, the
  // feed, and fair-play alerts beside it.
  const PAGE = 100;
  const DISMISSED = "flagfall.admin.dismissedAlerts";
  let items = $state<Submission[]>([]);
  let summary = $state<SubmissionSummary | null>(null);
  let more = $state(false);
  let error = $state("");
  let live = $state(true);
  // Filters from links (team and user pages, "Compare attempts").
  const teamId = route.query.get("teamId") ?? "";
  const userId = route.query.get("userId") ?? "";
  const challengeId = route.query.get("challengeId") ?? "";
  let correct = $state(route.query.get("correct") ?? "");
  let dismissed = $state<string[]>(readDismissed());

  const alerts = $derived((summary?.alerts ?? []).filter((a) => !dismissed.includes(a.id)));
  const pills = $derived([
    { v: "", label: "All", n: summary?.total },
    { v: "true", label: "Correct", n: summary?.correct },
    { v: "false", label: "Incorrect", n: summary?.incorrect },
  ]);

  function readDismissed(): string[] {
    try {
      const v = JSON.parse(localStorage.getItem(DISMISSED) ?? "[]");
      return Array.isArray(v) ? v : [];
    } catch {
      return [];
    }
  }

  function dismiss(a: FairPlayAlert) {
    // Keep the list short: only ids still being reported matter.
    const current = new Set((summary?.alerts ?? []).map((x) => x.id));
    dismissed = [...dismissed.filter((id) => current.has(id)), a.id];
    localStorage.setItem(DISMISSED, JSON.stringify(dismissed));
  }

  async function fetchPage(before?: string) {
    return api.get<Submission[]>("/submissions" + qs({ teamId, userId, challengeId, correct, before, limit: PAGE }));
  }

  async function load() {
    try {
      const [page, sum] = await Promise.all([
        fetchPage(),
        api.get<SubmissionSummary>("/submissions/summary" + qs({ teamId, userId, challengeId })),
      ]);
      items = page;
      summary = sum;
      more = page.length === PAGE;
      error = "";
    } catch (err) {
      error = message(err);
    }
  }

  async function older() {
    const last = items.at(-1);
    if (!last) return;
    const page = await fetchPage(last.id);
    items = [...items, ...page];
    more = page.length === PAGE;
  }

  function pick(v: string) {
    correct = v;
    load();
  }

  function names(teams: string[]): string {
    if (teams.length <= 1) return teams.join("");
    if (teams.length <= 4) return teams.slice(0, -1).join(", ") + " and " + teams.at(-1);
    return teams.slice(0, 3).join(", ") + ` and ${teams.length - 3} more`;
  }

  function within(seconds: number): string {
    if (seconds < 60) return "under a minute";
    const m = Math.round(seconds / 60);
    return m === 1 ? "1 minute" : `${m} minutes`;
  }

  onMount(() => {
    load();
    const t = setInterval(() => live && items.length <= PAGE && load(), 10_000);
    return () => clearInterval(t);
  });
</script>

<div class="page-head">
  <div>
    <h1>Submissions</h1>
    <p class="muted">Every flag attempt, newest first.</p>
  </div>
  {#if can("admin")}<a class="btn-line" href={"/admin/api/v1/export" + qs({ dataset: "submissions", format: "csv" })} download>Export CSV</a>{/if}
</div>
{#if error}<p class="notice bad">{error}</p>{/if}

<div class="row">
  <section class="grow-main" aria-label="Submission feed">
    <div class="toolbar">
      <div class="filters" role="group" aria-label="Filter by result">
        {#each pills as p (p.v)}
          <input type="radio" name="result" id={"f-" + (p.v || "all")} checked={correct === p.v} onchange={() => pick(p.v)} />
          <label for={"f-" + (p.v || "all")}>{p.label}{#if p.n !== undefined} <span class="mono">{p.n}</span>{/if}</label>
        {/each}
      </div>
      <label class="check"><input type="checkbox" bind:checked={live} /><span>Refresh every 10 s</span></label>
      {#if teamId || userId || challengeId}<a class="link" href={href("/submissions")}>Clear filters</a>{/if}
    </div>

    <div class="table-wrap feed-table">
      <table>
        <thead><tr><th scope="col">Time</th><th scope="col">Team</th><th scope="col">Challenge</th><th scope="col">Result</th><th scope="col">IP</th></tr></thead>
        <tbody>
          {#each items as s (s.id)}
            <tr class={s.correct ? "ok" : "bad"}>
              <td class="mono muted" title={when(s.submittedAt)}>{ago(s.submittedAt)}</td>
              <td>
                <a href={href(`/teams/${s.teamId}`)}><b>{s.teamName}</b></a>
                {#if s.userId}<div class="muted small"><a href={href(`/users/${s.userId}`)}>{s.userName}</a></div>{/if}
              </td>
              <td><a href={href(`/challenges/${s.challengeId}`)}>{s.challengeName}</a></td>
              <td>
                {#if s.correct}<span class="badge ok">Correct</span>
                {:else}<span class="badge bad">Incorrect</span> <span class="mono muted small" title="Submitted flag">{s.submitted}</span>{/if}
              </td>
              <td class="mono muted">{s.ip ?? "—"}</td>
            </tr>
          {:else}
            <tr><td colspan="5" class="empty">No submissions.</td></tr>
          {/each}
        </tbody>
      </table>
    </div>
    {#if more}<div class="actions"><button onclick={older}>Older</button></div>{/if}
  </section>

  <aside class="side-panel stack" aria-labelledby="alerts">
    <h2 id="alerts">Fair-play alerts</h2>
    {#each alerts as a (a.id)}
      {#if a.kind === "shared-flag"}
        <div class="panel pad warn stack-s">
          <b class="accent2">Same wrong flag from {a.count} teams</b>
          <p class="small">{names(a.teams)} sent an identical wrong flag for {a.challengeName} within {within(a.spanSeconds)}. This can mean a leaked or shared answer.</p>
          <div class="btn-row">
            <a class="btn-line" href={href("/submissions" + qs({ challengeId: a.challengeId, correct: "false" }))}>Compare attempts</a>
            <button class="link muted small" onclick={() => dismiss(a)}>Dismiss</button>
          </div>
        </div>
      {:else}
        <div class="panel pad stack-s">
          <b>Close to the rate limit</b>
          <p class="small">{a.teams[0]} sent {a.count} attempts in one minute on {a.challengeName} (limit {a.limit}). {a.count >= a.limit ? "They're being held back." : "No action needed yet."}</p>
          <div class="btn-row">
            <a class="btn-line" href={href("/submissions" + qs({ teamId: a.teamIds[0] }))}>See attempts</a>
            <button class="link muted small" onclick={() => dismiss(a)}>Dismiss</button>
          </div>
        </div>
      {/if}
    {:else}
      <p class="panel pad muted small">Nothing looks off. Alerts appear here when several teams send the same wrong flag, or a team nears the flag rate limit.</p>
    {/each}
  </aside>
</div>
