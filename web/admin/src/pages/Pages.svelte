<script lang="ts">
  import { onMount } from "svelte";
  import { api, fieldErrors, message } from "../lib/api";
  import { can, notify } from "../lib/state.svelte";
  import type { Page } from "../lib/types";
  import { when } from "../lib/util";
  import Markdown from "../components/Markdown.svelte";

  let list = $state<Page[]>([]);
  let error = $state("");
  let editing = $state<{ isNew: boolean; slug: string } | null>(null);
  let form = $state({ slug: "", title: "", body: "", visible: true, position: 0 });
  let errors = $state<Record<string, string>>({});
  const editable = $derived(can("admin"));

  async function load() {
    try {
      list = await api.get<Page[]>("/pages");
    } catch (err) {
      error = message(err);
    }
  }

  function edit(p?: Page) {
    editing = { isNew: !p, slug: p?.slug ?? "" };
    form = p ? { slug: p.slug, title: p.title, body: p.body, visible: p.visible, position: p.position } : { slug: "", title: "", body: "", visible: true, position: list.length };
    errors = {};
  }

  async function save(ev: SubmitEvent) {
    ev.preventDefault();
    if (!editing) return;
    const slug = editing.isNew ? form.slug.trim() : editing.slug;
    try {
      await api.put(`/pages/${encodeURIComponent(slug)}`, { title: form.title, body: form.body, visible: form.visible, position: form.position });
      notify("Page saved.");
      editing = null;
      await load();
    } catch (err) {
      errors = fieldErrors(err);
      notify(message(err), "error");
    }
  }

  async function remove(p: Page) {
    if (!confirm(`Delete the page "${p.title}"?`)) return;
    try {
      await api.del(`/pages/${p.slug}`);
      list = list.filter((x) => x.slug !== p.slug);
    } catch (err) {
      notify(message(err), "error");
    }
  }

  onMount(load);
</script>

<h1>Pages</h1>
<p class="muted">Extra pages for players, such as rules, FAQ or sponsors. Each one is at <code>/pages/&lt;slug&gt;</code>.</p>
{#if error}<p class="notice bad">{error}</p>{/if}
{#if editable}<div class="toolbar"><span class="spacer"></span><button class="primary" onclick={() => edit()}>New page</button></div>{/if}

{#if editing}
  <form class="card" onsubmit={save}>
    <div class="grid">
      <label><span>Title</span><input bind:value={form.title} maxlength="80" required />
        {#if errors.title}<div class="field-error">{errors.title}</div>{/if}</label>
      <label><span>Slug</span><input class="mono" bind:value={form.slug} disabled={!editing.isNew} required placeholder="rules" />
        {#if errors.slug}<div class="field-error">{errors.slug}</div>{/if}</label>
      <label><span>Position in menus</span><input type="number" min="0" bind:value={form.position} /></label>
    </div>
    <Markdown label="Content (Markdown)" bind:value={form.body} rows={14} />
    {#if errors.body}<div class="field-error">{errors.body}</div>{/if}
    <label class="check"><input type="checkbox" bind:checked={form.visible} /><span>Visible to players</span></label>
    <div class="actions"><button class="primary">Save page</button><button type="button" onclick={() => (editing = null)}>Cancel</button></div>
  </form>
{/if}

<table>
  <thead><tr><th>Title</th><th>Slug</th><th>Visible</th><th>Updated</th><th></th></tr></thead>
  <tbody>
    {#each list as p (p.slug)}
      <tr>
        <td>{p.title}</td>
        <td class="mono"><a href={`/pages/${p.slug}`} target="_blank" rel="noopener">{p.slug}</a></td>
        <td>{#if p.visible}<span class="badge ok">visible</span>{:else}<span class="badge">hidden</span>{/if}</td>
        <td class="muted">{when(p.updatedAt)}</td>
        <td class="num">{#if editable}<div class="actions" style="margin:0;justify-content:flex-end">
          <button class="small" onclick={() => edit(p)}>Edit</button>
          <button class="small danger" onclick={() => remove(p)}>Delete</button></div>{/if}</td>
      </tr>
    {:else}
      <tr><td colspan="5" class="empty">No pages yet.</td></tr>
    {/each}
  </tbody>
</table>
