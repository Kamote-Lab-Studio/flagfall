<script lang="ts">
  import { onMount } from "svelte";
  import { api, fieldErrors, message } from "../lib/api";
  import { href, navigate } from "../lib/router.svelte";
  import { can, notify } from "../lib/state.svelte";
  import type { Challenge } from "../lib/types";
  import { bytes, fromLocalInput, toLocalInput } from "../lib/util";
  import Markdown from "../components/Markdown.svelte";

  let { id }: { id: string } = $props();

  let c = $state<Challenge | null>(null);
  let error = $state("");
  let errors = $state<Record<string, string>>({});
  let busy = $state(false);
  let form = $state({
    slug: "", name: "", category: "", difficulty: "", author: "", description: "", connection: "",
    value: 0, dynamic: false, minValue: 0, decay: 10, state: "hidden", visibleFrom: "", tags: "", position: 0,
  });
  let flag = $state({ kind: "exact", value: "" });
  let hints = $state<{ id?: string; cost: number; content: string; unlocks: number }[]>([]);
  let uploading = $state(false);
  const editable = $derived(can("author"));

  function fill(x: Challenge) {
    c = x;
    form = {
      slug: x.slug, name: x.name, category: x.category, difficulty: x.difficulty ?? "", author: x.author,
      description: x.description, connection: x.connection ?? "", value: x.value,
      dynamic: x.minValue !== null && x.decay !== null, minValue: x.minValue ?? 0, decay: x.decay ?? 10,
      state: x.state, visibleFrom: toLocalInput(x.visibleFrom), tags: x.tags.join(", "), position: x.position,
    };
    hints = x.hints.map((h) => ({ ...h }));
  }

  async function load() {
    try {
      fill(await api.get<Challenge>(`/challenges/${id}`));
    } catch (err) {
      error = message(err);
    }
  }

  async function save(ev: SubmitEvent) {
    ev.preventDefault();
    busy = true;
    errors = {};
    try {
      fill(await api.patch<Challenge>(`/challenges/${id}`, {
        slug: form.slug, name: form.name, category: form.category, difficulty: form.difficulty || null,
        author: form.author, description: form.description, connection: form.connection || null, value: form.value,
        minValue: form.dynamic ? form.minValue : null, decay: form.dynamic ? form.decay : null,
        state: form.state, visibleFrom: fromLocalInput(form.visibleFrom), position: form.position,
        tags: form.tags.split(",").map((t) => t.trim()).filter(Boolean),
      }));
      notify("Challenge saved.");
    } catch (err) {
      errors = fieldErrors(err);
      notify(message(err), "error");
    } finally {
      busy = false;
    }
  }

  async function addFlag(ev: SubmitEvent) {
    ev.preventDefault();
    try {
      fill(await api.post<Challenge>(`/challenges/${id}/flags`, flag));
      flag = { kind: flag.kind, value: "" };
    } catch (err) {
      notify(message(err), "error");
    }
  }

  async function deleteFlag(flagId: string) {
    if (!confirm("Delete this flag?")) return;
    try {
      await api.del(`/challenges/${id}/flags/${flagId}`);
      await load();
    } catch (err) {
      notify(message(err), "error");
    }
  }

  async function saveHints() {
    try {
      fill(await api.put<Challenge>(`/challenges/${id}/hints`, hints.map((h) => ({ id: h.id, cost: h.cost, content: h.content }))));
      notify("Hints saved.");
    } catch (err) {
      notify(message(err), "error");
    }
  }

  async function upload(ev: Event) {
    const input = ev.currentTarget as HTMLInputElement;
    const files = [...(input.files ?? [])];
    input.value = "";
    uploading = true;
    try {
      for (const f of files) fill(await api.upload<Challenge>(`/challenges/${id}/files`, "file", f));
    } catch (err) {
      notify(message(err), "error");
    } finally {
      uploading = false;
    }
  }

  async function deleteFile(fileId: string, name: string) {
    if (!confirm(`Delete ${name}?`)) return;
    try {
      await api.del(`/challenges/${id}/files/${fileId}`);
      await load();
    } catch (err) {
      notify(message(err), "error");
    }
  }

  async function remove() {
    if (!c || !confirm(`Delete "${c.name}"? This can't be undone.`)) return;
    try {
      await api.del(`/challenges/${id}`);
      notify("Challenge deleted.");
      navigate("/challenges");
    } catch (err) {
      notify(message(err), "error");
    }
  }

  onMount(load);
</script>

<p><a href={href("/challenges")}>← Challenges</a></p>
{#if error}<p class="notice bad">{error}</p>{/if}
{#if c}
  <h1>{c.name} <span class="badge" class:ok={c.state === "visible"} class:warn={c.state === "hidden"}>{c.state}</span></h1>
  <p class="muted">{c.solves} solves · {c.attempts} attempts · worth {c.currentValue} now</p>

  {#if c.state === "visible" && c.flags.length === 0}
    <div class="notice">This challenge is visible but has no flag, so nobody can solve it.</div>
  {/if}

  <form class="card" onsubmit={save}>
    <fieldset disabled={!editable}>
      <div class="grid">
        <label><span>Name</span><input bind:value={form.name} required />
          {#if errors.name}<div class="field-error">{errors.name}</div>{/if}</label>
        <label><span>Slug</span><input class="mono" bind:value={form.slug} required />
          {#if errors.slug}<div class="field-error">{errors.slug}</div>{/if}</label>
        <label><span>Category</span><input bind:value={form.category} required />
          {#if errors.category}<div class="field-error">{errors.category}</div>{/if}</label>
        <label><span>Difficulty</span>
          <select bind:value={form.difficulty}>
            <option value="">—</option><option value="easy">Easy</option><option value="medium">Medium</option>
            <option value="hard">Hard</option><option value="insane">Insane</option>
          </select></label>
        <label><span>Author</span><input bind:value={form.author} /></label>
        <label><span>State</span>
          <select bind:value={form.state}>
            <option value="hidden">Hidden</option><option value="visible">Visible</option><option value="retired">Retired</option>
          </select>
          {#if errors.state}<div class="field-error">{errors.state}</div>{/if}</label>
        <label><span>Visible from (optional)</span><input type="datetime-local" bind:value={form.visibleFrom} />
          {#if errors.visibleFrom}<div class="field-error">{errors.visibleFrom}</div>{/if}</label>
        <label><span>Position</span><input type="number" min="0" bind:value={form.position} /></label>
      </div>
      <label><span>Connection (e.g. <code>nc host 1337</code> or a URL)</span><input class="mono" bind:value={form.connection} />
        {#if errors.connection}<div class="field-error">{errors.connection}</div>{/if}</label>
      <label><span>Tags (comma-separated)</span><input bind:value={form.tags} />
        {#if errors.tags}<div class="field-error">{errors.tags}</div>{/if}</label>

      <div class="grid">
        <label><span>Points</span><input type="number" min="0" bind:value={form.value} />
          {#if errors.value}<div class="field-error">{errors.value}</div>{/if}</label>
        {#if form.dynamic}
          <label><span>Minimum points</span><input type="number" min="0" bind:value={form.minValue} />
            {#if errors.minValue}<div class="field-error">{errors.minValue}</div>{/if}</label>
          <label><span>Solves to reach the minimum</span><input type="number" min="1" bind:value={form.decay} />
            {#if errors.decay}<div class="field-error">{errors.decay}</div>{/if}</label>
        {/if}
      </div>
      <label class="check"><input type="checkbox" bind:checked={form.dynamic} /><span>Points drop as teams solve it (dynamic)</span></label>

      <Markdown label="Description (Markdown)" bind:value={form.description} />
      {#if errors.description}<div class="field-error">{errors.description}</div>{/if}

      {#if editable}
        <div class="actions">
          <button class="primary" disabled={busy}>Save challenge</button>
          <span class="spacer"></span>
          <button type="button" class="danger" onclick={remove} disabled={c.solves > 0} title={c.solves > 0 ? "Solved challenges can't be deleted; retire it instead." : undefined}>Delete</button>
        </div>
      {/if}
    </fieldset>
  </form>

  <h2>Flags</h2>
  <div class="card">
    {#if c.flags.length === 0}<p class="empty">No flags yet.</p>{/if}
    <table>
      <tbody>
        {#each c.flags as f (f.id)}
          <tr>
            <td><span class="badge">{f.kind}</span></td>
            <td class="mono">{f.pattern ?? "(stored hashed)"}</td>
            <td class="num">{#if editable}<button class="small danger" onclick={() => deleteFlag(f.id)}>Delete</button>{/if}</td>
          </tr>
        {/each}
      </tbody>
    </table>
    {#if editable}
      <form class="toolbar" style="margin:0.75rem 0 0" onsubmit={addFlag}>
        <select bind:value={flag.kind} aria-label="Flag kind">
          <option value="exact">Exact</option><option value="case-insensitive">Case-insensitive</option><option value="pattern">Pattern (regex)</option>
        </select>
        <input class="mono" style="flex:1" aria-label="Flag" placeholder={flag.kind === "pattern" ? "^flag\\{[a-f0-9]{16}\\}$" : "flag{...}"} bind:value={flag.value} required />
        <button>Add flag</button>
      </form>
      <p class="hint">Exact and case-insensitive flags are stored hashed, so they can't be shown again.</p>
    {/if}
  </div>

  <h2>Hints</h2>
  <div class="card">
    {#each hints as h, i (h.id ?? `new-${i}`)}
      <div class="hint-row">
        <label><span>Cost</span><input type="number" min="0" bind:value={h.cost} disabled={!editable} /></label>
        <label class="grow"><span>Hint {i + 1}{h.unlocks ? ` · unlocked by ${h.unlocks} teams` : ""}</span>
          <textarea rows="2" bind:value={h.content} disabled={!editable}></textarea></label>
        {#if editable}<button class="small danger" onclick={() => hints.splice(i, 1)}>Remove</button>{/if}
      </div>
    {:else}
      <p class="empty">No hints.</p>
    {/each}
    {#if editable}
      <div class="actions">
        <button onclick={() => hints.push({ cost: 0, content: "", unlocks: 0 })}>Add hint</button>
        <button class="primary" onclick={saveHints}>Save hints</button>
      </div>
    {/if}
  </div>

  <h2>Files</h2>
  <div class="card">
    {#if c.files.length === 0}<p class="empty">No files.</p>{/if}
    <table>
      <tbody>
        {#each c.files as f (f.id)}
          <tr>
            <td><a href={f.url} download>{f.name}</a></td>
            <td class="num">{bytes(f.size)}</td>
            <td class="mono muted" title={f.sha256}>{f.sha256.slice(0, 12)}…</td>
            <td class="num">{#if editable}<button class="small danger" onclick={() => deleteFile(f.id, f.name)}>Delete</button>{/if}</td>
          </tr>
        {/each}
      </tbody>
    </table>
    {#if editable}
      <label class="button" style="margin-top:0.75rem;display:inline-block">
        {uploading ? "Uploading…" : "Upload files"}
        <input class="sr-only" type="file" multiple onchange={upload} disabled={uploading} />
      </label>
    {/if}
  </div>
{/if}

<style>
  fieldset { border: 0; margin: 0; padding: 0; min-width: 0; }
  .hint-row { display: flex; gap: 0.75rem; align-items: flex-start; }
  .hint-row label:first-child { width: 6rem; }
  .hint-row .grow { flex: 1; }
  .hint-row button { margin-top: 1.6rem; }
  .hint-row textarea { min-height: 3rem; }
  .spacer { flex: 1; }
</style>
