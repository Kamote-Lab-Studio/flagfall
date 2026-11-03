<script lang="ts">
  import { onMount } from "svelte";
  import { api, ApiError, fieldErrors, message } from "../lib/api";
  import { href, navigate } from "../lib/router.svelte";
  import { can, notify } from "../lib/state.svelte";
  import type { Challenge, ChallengeSummary } from "../lib/types";

  let list = $state<ChallengeSummary[] | null>(null);
  let filter = $state("");
  let error = $state("");
  let creating = $state(false);
  let draft = $state({ slug: "", name: "", category: "", value: 100 });
  let errors = $state<Record<string, string>>({});
  type Problem = { path: string; message: string };
  let problems = $state<Problem[]>([]);
  let importWarnings = $state<Problem[]>([]);
  let importing = $state(false);

  const shown = $derived(
    (list ?? []).filter((c) => !filter || `${c.name} ${c.slug} ${c.category}`.toLowerCase().includes(filter.toLowerCase())),
  );

  async function load() {
    try {
      list = await api.get<ChallengeSummary[]>("/challenges");
    } catch (err) {
      error = message(err);
    }
  }

  async function create(ev: SubmitEvent) {
    ev.preventDefault();
    errors = {};
    try {
      const c = await api.post<Challenge>("/challenges", draft);
      navigate(`/challenges/${c.id}`);
    } catch (err) {
      errors = fieldErrors(err);
      if (!Object.keys(errors).length) notify(message(err), "error");
    }
  }

  async function importZip(ev: Event) {
    const input = ev.currentTarget as HTMLInputElement;
    const file = input.files?.[0];
    input.value = "";
    if (!file) return;
    importing = true;
    problems = [];
    importWarnings = [];
    try {
      const r = await api.upload<{ created: string[] | null; updated: string[] | null; warnings?: Problem[] }>("/challenges/import", "file", file);
      notify(`Imported: ${r.created?.length ?? 0} new, ${r.updated?.length ?? 0} updated.`);
      importWarnings = r.warnings ?? [];
      await load();
    } catch (err) {
      if (err instanceof ApiError && err.problems.length) problems = err.problems;
      else notify(message(err), "error");
    } finally {
      importing = false;
    }
  }

  onMount(load);
</script>

<h1>Challenges</h1>
{#if error}<p class="notice bad">{error}</p>{/if}

<div class="toolbar">
  <input type="search" placeholder="Filter" aria-label="Filter challenges" bind:value={filter} />
  <span class="spacer"></span>
  {#if can("author")}
    <label class="button" style="margin:0">
      {importing ? "Importing…" : "Import .zip"}
      <input class="sr-only" type="file" accept=".zip,application/zip" onchange={importZip} disabled={importing} />
    </label>
    <button class="primary" onclick={() => (creating = !creating)}>New challenge</button>
  {/if}
</div>

{#if problems.length}
  <div class="notice bad">
    <p><b>The pack has problems; nothing was imported.</b></p>
    <ul>{#each problems as p, i (i)}<li><code>{p.path}</code>: {p.message}</li>{/each}</ul>
  </div>
{/if}
{#if importWarnings.length}
  <div class="notice">
    <p><b>Imported. Notes about theme fields:</b></p>
    <ul>{#each importWarnings as p, i (i)}<li>{#if p.path}<code>{p.path}</code>: {/if}{p.message}</li>{/each}</ul>
  </div>
{/if}

{#if creating}
  <form class="card" onsubmit={create}>
    <div class="grid">
      <label><span>Name</span><input bind:value={draft.name} required />
        {#if errors.name}<div class="field-error">{errors.name}</div>{/if}</label>
      <label><span>Slug</span><input class="mono" bind:value={draft.slug} required placeholder="web-100" />
        {#if errors.slug}<div class="field-error">{errors.slug}</div>{/if}</label>
      <label><span>Category</span><input bind:value={draft.category} required />
        {#if errors.category}<div class="field-error">{errors.category}</div>{/if}</label>
      <label><span>Points</span><input type="number" min="0" bind:value={draft.value} />
        {#if errors.value}<div class="field-error">{errors.value}</div>{/if}</label>
    </div>
    <p class="hint">New challenges start hidden. Add a flag, then make it visible.</p>
    <div class="actions"><button class="primary">Create</button><button type="button" onclick={() => (creating = false)}>Cancel</button></div>
  </form>
{/if}

{#if list}
  {#if shown.length === 0}
    <p class="empty">{list.length ? "Nothing matches." : "No challenges yet."}</p>
  {:else}
    <table>
      <thead><tr><th>Name</th><th>Category</th><th>Difficulty</th><th>State</th><th class="num">Points</th><th class="num">Solves</th><th class="num">Attempts</th></tr></thead>
      <tbody>
        {#each shown as c (c.id)}
          <tr>
            <td><a href={href(`/challenges/${c.id}`)}>{c.name}</a> <span class="muted mono">{c.slug}</span></td>
            <td>{c.category}</td>
            <td>{c.difficulty ?? "—"}</td>
            <td><span class="badge" class:ok={c.state === "visible"} class:warn={c.state === "hidden"}>{c.state}</span></td>
            <td class="num">{c.currentValue}{#if c.currentValue !== c.value}<span class="muted"> / {c.value}</span>{/if}</td>
            <td class="num">{c.solves}</td>
            <td class="num">{c.attempts}</td>
          </tr>
        {/each}
      </tbody>
    </table>
  {/if}
{/if}
