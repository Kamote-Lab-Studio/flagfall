<script lang="ts">
  import { onMount } from "svelte";
  import { api, fieldErrors, message } from "../lib/api";
  import { notify } from "../lib/state.svelte";
  import type { Announcement } from "../lib/types";
  import { fromLocalInput, toLocalInput, when } from "../lib/util";
  import Markdown from "../components/Markdown.svelte";

  let list = $state<Announcement[]>([]);
  let error = $state("");
  let editing = $state<string | null>(null); // "new" or an id
  let form = $state({ title: "", body: "", pinned: false, postAt: "" });
  let errors = $state<Record<string, string>>({});

  async function load() {
    try {
      list = await api.get<Announcement[]>("/announcements");
    } catch (err) {
      error = message(err);
    }
  }

  function edit(a?: Announcement) {
    editing = a ? a.id : "new";
    form = a ? { title: a.title, body: a.body, pinned: a.pinned, postAt: a.posted ? "" : toLocalInput(a.postAt) } : { title: "", body: "", pinned: false, postAt: "" };
    errors = {};
  }

  async function save(ev: SubmitEvent) {
    ev.preventDefault();
    const body: Record<string, unknown> = { title: form.title, body: form.body, pinned: form.pinned };
    // Empty means "now" for new ones; for posted ones leave the time alone.
    const existing = list.find((a) => a.id === editing);
    if (form.postAt) body.postAt = fromLocalInput(form.postAt);
    else if (!existing || !existing.posted) body.postAt = null;
    try {
      if (editing === "new") await api.post("/announcements", body);
      else await api.patch(`/announcements/${editing}`, body);
      notify(form.postAt && new Date(form.postAt) > new Date() ? "Announcement scheduled." : "Announcement posted.");
      editing = null;
      await load();
    } catch (err) {
      errors = fieldErrors(err);
      notify(message(err), "error");
    }
  }

  async function remove(a: Announcement) {
    if (!confirm(`Delete "${a.title}"?`)) return;
    try {
      await api.del(`/announcements/${a.id}`);
      list = list.filter((x) => x.id !== a.id);
    } catch (err) {
      notify(message(err), "error");
    }
  }

  onMount(load);
</script>

<h1>Announcements</h1>
{#if error}<p class="notice bad">{error}</p>{/if}
<div class="toolbar"><span class="spacer"></span><button class="primary" onclick={() => edit()}>New announcement</button></div>

{#if editing}
  <form class="card" onsubmit={save}>
    <label><span>Title</span><input bind:value={form.title} maxlength="120" required />
      {#if errors.title}<div class="field-error">{errors.title}</div>{/if}</label>
    <Markdown label="Message (Markdown)" bind:value={form.body} rows={6} />
    {#if errors.body}<div class="field-error">{errors.body}</div>{/if}
    <div class="grid">
      <label><span>Post at (empty = now)</span><input type="datetime-local" bind:value={form.postAt} />
        {#if errors.postAt}<div class="field-error">{errors.postAt}</div>{/if}</label>
    </div>
    <label class="check"><input type="checkbox" bind:checked={form.pinned} /><span>Pinned</span></label>
    <p class="hint">Players online see new announcements right away.</p>
    <div class="actions"><button class="primary">{editing === "new" ? "Post" : "Save"}</button><button type="button" onclick={() => (editing = null)}>Cancel</button></div>
  </form>
{/if}

{#each list as a (a.id)}
  <article class="card">
    <div class="actions" style="margin:0 0 0.5rem">
      <b>{a.title}</b>
      {#if a.pinned}<span class="badge">pinned</span>{/if}
      {#if !a.posted}<span class="badge warn">scheduled {when(a.postAt)}</span>{:else}<span class="muted">{when(a.postAt)}</span>{/if}
      <span class="spacer"></span>
      <button class="small" onclick={() => edit(a)}>Edit</button>
      <button class="small danger" onclick={() => remove(a)}>Delete</button>
    </div>
    <!-- Sanitized server-side. -->
    <div class="rendered">{@html a.bodyHtml}</div>
  </article>
{:else}
  <p class="empty">No announcements yet.</p>
{/each}
