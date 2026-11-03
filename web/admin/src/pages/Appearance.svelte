<script lang="ts">
  import { onMount, untrack } from "svelte";
  import { api, fieldErrors, message } from "../lib/api";
  import { notify } from "../lib/state.svelte";
  import { contrastRatio } from "../lib/contrast";
  import type { InstalledTheme, SettingValue, ThemeHistoryEntry, ThemeList, ThemeSetting } from "../lib/types";
  import { fromLocalInput, toLocalInput, when } from "../lib/util";

  // Open preview tabs reload when this channel says the draft changed.
  // Kept in step with PREVIEW_CHANNEL in web/sdk/src/theme.ts.
  const PREVIEW_CHANNEL = "flagfall-theme-preview";
  const channel = typeof BroadcastChannel !== "undefined" ? new BroadcastChannel(PREVIEW_CHANNEL) : null;

  let data = $state<ThemeList | null>(null);
  let error = $state("");
  let selectedId = $state("");
  // Loosely typed because Svelte can't bind through a type cast.
  let values = $state<Record<string, any>>({});
  let customCss = $state("");
  let errors = $state<Record<string, string>>({});
  let dirty = $state(false);
  let busy = $state(false);
  let scheduleAt = $state("");
  let history = $state<ThemeHistoryEntry[]>([]);

  const selected = $derived(data?.themes.find((t) => t.id === selectedId) ?? null);
  const groups = $derived.by(() => {
    const out: { name: string; settings: ThemeSetting[] }[] = [];
    for (const s of selected?.settings ?? []) {
      const name = s.group || "Settings";
      let g = out.find((x) => x.name === name);
      if (!g) out.push((g = { name, settings: [] }));
      g.settings.push(s);
    }
    return out;
  });

  async function load(keepSelection = true) {
    try {
      data = await api.get<ThemeList>("/themes");
      if (!keepSelection || !data.themes.some((t) => t.id === selectedId)) {
        selectedId = data.themes.find((t) => t.previewing)?.id ?? data.liveThemeId;
      }
      error = "";
    } catch (err) {
      error = message(err);
    }
  }

  // Reset the form when another theme is picked (not on every reload).
  $effect(() => {
    selectedId;
    untrack(() => {
      if (selected) startEditing(selected);
    });
  });

  function startEditing(t: InstalledTheme) {
    const base = t.draft ?? t.published ?? null;
    const v: Record<string, SettingValue> = {};
    for (const s of t.settings) {
      const stored = base?.values[s.key];
      v[s.key] = stored ?? s.default ?? (s.type === "boolean" ? false : "");
    }
    values = v;
    customCss = base?.customCss ?? "";
    errors = {};
    dirty = false;
    scheduleAt = "";
    loadHistory(t.id);
  }

  async function loadHistory(id: string) {
    try {
      history = (await api.get<{ versions: ThemeHistoryEntry[] }>(`/themes/${id}/versions`)).versions;
    } catch {
      history = [];
    }
  }

  // Only values that differ from the theme's defaults are stored, so a
  // theme update that changes a default still reaches organizers who never
  // touched that setting.
  function changedValues(): Record<string, SettingValue> {
    const out: Record<string, SettingValue> = {};
    for (const s of selected?.settings ?? []) {
      let v = values[s.key];
      if (s.type === "number" && typeof v === "string") v = v === "" ? (s.default ?? 0) : Number(v);
      if (s.type === "color" && typeof v === "string") v = v.toUpperCase();
      const d = s.type === "color" && typeof s.default === "string" ? s.default.toUpperCase() : s.default;
      if (v !== d) out[s.key] = v;
    }
    return out;
  }

  function contrastWarning(s: ThemeSetting): string {
    if (s.type !== "color" || !s.contrastWith) return "";
    const other = selected?.settings.find((x) => x.key === s.contrastWith);
    const ratio = contrastRatio(String(values[s.key]), String(values[s.contrastWith]));
    const min = s.minContrast ?? 4.5;
    if (ratio === null || ratio >= min) return "";
    return `Contrast with ${other?.label ?? s.contrastWith} is ${(Math.floor(ratio * 10) / 10).toFixed(1)}:1; it needs at least ${min}:1.`;
  }

  function options(s: ThemeSetting): { value: string; label: string }[] {
    return (s.options ?? []).map((o) => (typeof o === "string" ? { value: o, label: o } : o));
  }

  function changed() {
    dirty = true;
  }

  // Uploads the image, then puts its URL in the setting; saving the draft
  // (or publishing) is what makes it count.
  async function uploadImage(s: ThemeSetting, ev: Event) {
    const input = ev.currentTarget as HTMLInputElement;
    const file = input.files?.[0];
    if (!file || !selected) return;
    try {
      const res = await api.upload<{ url: string }>(`/themes/${selected.id}/images/${s.key}`, "file", file);
      values[s.key] = res.url;
      errors = { ...errors, [`settings.${s.key}`]: "" };
      changed();
    } catch (err) {
      errors = { ...errors, [`settings.${s.key}`]: fieldErrors(err).file ?? message(err) };
    } finally {
      input.value = "";
    }
  }

  async function saveDraft(): Promise<boolean> {
    if (!selected) return false;
    busy = true;
    errors = {};
    try {
      await api.put(`/themes/${selected.id}/draft`, { settings: changedValues(), customCss });
      channel?.postMessage({ type: "draft-saved", themeId: selected.id });
      dirty = false;
      await load();
      return true;
    } catch (err) {
      errors = fieldErrors(err);
      notify(message(err), "error");
      return false;
    } finally {
      busy = false;
    }
  }

  async function save(ev: SubmitEvent) {
    ev.preventDefault();
    if (await saveDraft()) notify("Draft saved. Players don't see it until you publish.");
  }

  async function preview() {
    if (!selected) return;
    // Open the tab now, while the click still counts as a user action.
    const tab = window.open("about:blank", "flagfall-preview");
    if (dirty && !(await saveDraft())) {
      tab?.close();
      return;
    }
    try {
      await api.post(`/themes/${selected.id}/preview`);
      if (tab) tab.location.href = "/";
      await load();
    } catch (err) {
      tab?.close();
      notify(message(err), "error");
    }
  }

  async function stopPreview() {
    if (!selected) return;
    try {
      await api.del(`/themes/${selected.id}/preview`);
      channel?.postMessage({ type: "preview-ended", themeId: selected.id });
      await load();
    } catch (err) {
      notify(message(err), "error");
    }
  }

  async function publish() {
    if (!selected) return;
    const what = selected.live ? "these changes" : `the ${selected.name} theme`;
    if (!confirm(`Publish ${what}? Open player tabs reload right away.`)) return;
    if (dirty && !(await saveDraft())) return;
    busy = true;
    try {
      await api.post(`/themes/${selected.id}/publish`, {});
      channel?.postMessage({ type: "published", themeId: selected.id });
      notify("Published. Players now see the new look.");
      await load();
      startEditing(selected);
    } catch (err) {
      notify(message(err), "error");
    } finally {
      busy = false;
    }
  }

  async function schedule() {
    if (!selected) return;
    const at = fromLocalInput(scheduleAt);
    if (!at || new Date(at) <= new Date()) {
      notify("Pick a time in the future.", "error");
      return;
    }
    if (dirty && !(await saveDraft())) return;
    try {
      await api.post(`/themes/${selected.id}/publish`, { publishAt: at });
      notify(`Scheduled for ${when(at)}.`);
      scheduleAt = "";
      await load();
    } catch (err) {
      notify(message(err), "error");
    }
  }

  async function cancelSchedule() {
    if (!selected) return;
    try {
      await api.del(`/themes/${selected.id}/schedule`);
      notify("Scheduled switch cancelled; it's a draft again.");
      await load();
      startEditing(selected);
    } catch (err) {
      notify(message(err), "error");
    }
  }

  async function discardDraft() {
    if (!selected || !confirm("Discard the draft? The form goes back to the published settings.")) return;
    try {
      await api.del(`/themes/${selected.id}/draft`);
      channel?.postMessage({ type: "draft-saved", themeId: selected.id });
      await load();
      startEditing(selected);
    } catch (err) {
      notify(message(err), "error");
    }
  }

  async function restore(v: ThemeHistoryEntry) {
    if (!selected || !confirm(`Publish version ${v.version} again? Open player tabs reload right away.`)) return;
    try {
      await api.post(`/themes/${selected.id}/versions/${v.version}/restore`);
      channel?.postMessage({ type: "published", themeId: selected.id });
      notify(`Version ${v.version} is live again.`);
      await load();
      startEditing(selected);
    } catch (err) {
      notify(message(err), "error");
    }
  }

  function status(t: InstalledTheme): string[] {
    const out: string[] = [];
    if (t.live) out.push("live");
    if (t.previewing) out.push("previewing");
    if (t.scheduled) out.push("scheduled");
    if (t.draft) out.push("draft");
    if (!t.built) out.push("not built");
    return out;
  }

  onMount(() => {
    load(false);
    return () => channel?.close();
  });
</script>

<h1>Appearance</h1>
<p class="muted">Pick the theme players see, customize it, preview your changes, then publish. Every published version is kept, so you can roll back.</p>
{#if error}<p class="notice bad">{error}</p>{/if}
{#if data?.fallback}
  <p class="notice bad">The published theme can't be served (it isn't installed or isn't built), so players see the default theme. Publish a theme to fix this.</p>
{/if}

{#if data}
  <div class="themes" role="group" aria-label="Installed themes">
    {#each data.themes as t (t.id)}
      <button type="button" class="theme" class:selected={t.id === selectedId} aria-pressed={t.id === selectedId} onclick={() => (selectedId = t.id)}>
        {#if t.screenshots[0]}<img src={t.screenshots[0].url} alt={t.screenshots[0].alt} />{/if}
        <b>{t.name}</b>
        <span class="muted small">v{t.version} · {t.source === "bundled" ? "bundled" : "uploaded"}</span>
        <span class="desc">{t.description}</span>
        <span class="badges">{#each status(t) as s}<span class="badge" class:ok={s === "live"} class:warn={s === "previewing" || s === "scheduled"} class:bad={s === "not built"}>{s}</span>{/each}</span>
      </button>
    {/each}
  </div>
{/if}

{#if selected}
  <form class="card" onsubmit={save} oninput={changed}>
    <div class="head">
      <h2>{selected.name}</h2>
      <span class="spacer"></span>
      {#if selected.previewing}
        <button type="button" onclick={stopPreview}>Stop preview</button>
      {/if}
      <button type="button" onclick={preview} disabled={busy || !selected.built}>{selected.previewing ? "Open preview" : "Preview"}</button>
    </div>
    {#if !selected.built}<p class="notice bad">This theme isn't built, so it can't be previewed or published.</p>{/if}
    {#if selected.scheduled}
      <p class="notice">Scheduled to go live {when(selected.scheduled.publishAt)}. <button type="button" class="small" onclick={cancelSchedule}>Cancel</button></p>
    {/if}
    {#if selected.draft}<p class="hint">You're editing an unpublished draft (version {selected.draft.version}).{#if !selected.live} Publishing switches players to this theme.{/if}</p>{/if}
    {#if selected.settings.length === 0}<p class="muted">This theme has no settings.</p>{/if}

    {#each groups as g (g.name)}
      <fieldset>
        <legend>{g.name}</legend>
        <div class="grid">
          {#each g.settings as s (s.key)}
            {@const err = errors[`settings.${s.key}`] || contrastWarning(s)}
            {#if s.type === "boolean"}
              <label class="check"><input type="checkbox" bind:checked={values[s.key]} /><span>{s.label}</span></label>
            {:else if s.type === "image"}
              <div class="setting">
                <span class="lbl" id={`img-${s.key}`}>{s.label}</span>
                <div class="image">
                  {#if values[s.key]}<img src={values[s.key]} alt={`Current ${s.label.toLowerCase()}`} />{:else}<span class="none muted">None</span>{/if}
                  <input type="file" aria-labelledby={`img-${s.key}`} accept={(s.accept ?? ["image/png", "image/svg+xml", "image/webp", "image/jpeg"]).join(",")} onchange={(ev) => uploadImage(s, ev)} />
                  {#if values[s.key]}<button type="button" class="small" onclick={() => { values[s.key] = ""; changed(); }}>Remove</button>{/if}
                </div>
                {#if s.help}<span class="hint">{s.help}</span>{/if}
                {#if err}<span class="field-error">{err}</span>{/if}
              </div>
            {:else}
              <label class:wide={s.multiline}>
                <span>{s.label}</span>
                {#if s.type === "color"}
                  <span class="color">
                    <input type="color" aria-label={`${s.label} picker`} bind:value={values[s.key]} />
                    <input class="mono" bind:value={values[s.key]} pattern={"#[0-9A-Fa-f]{6}"} maxlength="7" required />
                  </span>
                {:else if s.type === "select" || s.type === "font"}
                  <select bind:value={values[s.key]}>
                    {#each options(s) as o (o.value)}<option value={o.value}>{o.label}</option>{/each}
                  </select>
                {:else if s.type === "number"}
                  <input type="number" min={s.min} max={s.max} step={s.step ?? "any"} bind:value={values[s.key]} />
                {:else if s.multiline}
                  <textarea rows="3" maxlength={s.maxLength} bind:value={values[s.key]}></textarea>
                {:else}
                  <input maxlength={s.maxLength} bind:value={values[s.key]} />
                {/if}
                {#if s.help}<span class="hint">{s.help}</span>{/if}
                {#if err}<span class="field-error">{err}</span>{/if}
              </label>
            {/if}
          {/each}
        </div>
      </fieldset>
    {/each}

    <fieldset>
      <legend>Custom CSS</legend>
      <label class="wide">
        <span>Small overrides added after the theme's own styles. Only files from this server can be loaded.</span>
        <textarea rows="6" bind:value={customCss} spellcheck="false" placeholder=".board h1 {'{'} letter-spacing: 0.05em; {'}'}"></textarea>
        {#if errors.customCss}<span class="field-error">{errors.customCss}</span>{/if}
      </label>
    </fieldset>

    <div class="actions">
      <button class="primary" type="button" onclick={publish} disabled={busy || !selected.built}>{selected.live ? "Publish changes" : "Publish this theme"}</button>
      <button disabled={busy}>Save draft</button>
      {#if selected.draft}<button type="button" class="danger" onclick={discardDraft}>Discard draft</button>{/if}
      <span class="spacer"></span>
      <label class="inline"><span class="sr-only">Publish at</span><input type="datetime-local" bind:value={scheduleAt} min={toLocalInput(new Date().toISOString())} /></label>
      <button type="button" onclick={schedule} disabled={!scheduleAt || !selected.built}>Schedule</button>
    </div>
    {#if dirty}<p class="hint">Unsaved changes.</p>{/if}
  </form>

  <h2>History</h2>
  {#if history.length === 0}
    <p class="empty">Nothing saved for this theme yet.</p>
  {:else}
    <table>
      <thead><tr><th class="num">Version</th><th>Status</th><th>Published</th><th>By</th><th></th></tr></thead>
      <tbody>
        {#each history as v (v.version)}
          <tr>
            <td class="num">{v.version}</td>
            <td><span class="badge" class:ok={v.status === "live"} class:warn={v.status === "scheduled" || v.status === "draft"}>{v.status}</span></td>
            <td>{v.status === "scheduled" ? `at ${when(v.publishAt)}` : when(v.publishedAt)}</td>
            <td>{v.publishedBy ?? "—"}</td>
            <td>{#if v.status === "archived" && selected.built}<button class="small" onclick={() => restore(v)}>Restore</button>{/if}</td>
          </tr>
        {/each}
      </tbody>
    </table>
  {/if}
{/if}

<style>
  .themes { display: grid; grid-template-columns: repeat(auto-fill, minmax(15rem, 1fr)); gap: 0.75rem; margin-bottom: 1rem; }
  .theme { display: flex; flex-direction: column; gap: 0.25rem; text-align: left; padding: 0.85rem 1rem; background: var(--panel); }
  .theme.selected { border-color: var(--accent); box-shadow: inset 0 0 0 1px var(--accent); }
  .theme img { width: 100%; aspect-ratio: 16 / 10; object-fit: cover; border-radius: var(--radius); border: 1px solid var(--line); margin-bottom: 0.25rem; }
  .theme .desc { color: var(--muted); font-size: 0.85rem; font-weight: 400; }
  .small { font-size: 0.8rem; }
  .badges { display: flex; gap: 0.25rem; flex-wrap: wrap; margin-top: 0.25rem; }
  .head { display: flex; align-items: center; gap: 0.5rem; margin-bottom: 0.5rem; }
  .head h2 { margin: 0; }
  .spacer { flex: 1; }
  fieldset { border: 1px solid var(--line); border-radius: var(--radius); padding: 0.75rem 1rem 0.25rem; margin: 0 0 1rem; }
  legend { color: var(--muted); font-size: 0.85rem; padding: 0 0.35rem; }
  .wide { grid-column: 1 / -1; }
  .color { display: flex; gap: 0.4rem; }
  .color input[type="color"] { width: 2.6rem; padding: 0.1rem; height: auto; }
  .image { display: flex; align-items: center; gap: 0.6rem; }
  .image img { width: 2.75rem; height: 2.75rem; object-fit: contain; background: var(--bg); border: 1px solid var(--line); border-radius: var(--radius); }
  .image .none { width: 2.75rem; text-align: center; font-size: 0.8rem; }
  .image input[type="file"] { width: auto; flex: 1; min-width: 0; }
  .setting .hint, .setting .field-error { display: block; margin-top: 0.25rem; }
  .lbl { display: block; color: var(--muted); font-size: 0.85rem; margin-bottom: 0.25rem; }
  label .hint, label .field-error { display: block; margin-top: 0.25rem; }
  .inline { margin: 0; }
  .inline input { width: auto; }
</style>
