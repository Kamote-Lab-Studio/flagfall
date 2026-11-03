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
      // Uppercase colors, so the accent swatches match saved values.
      if (s.type === "color" && typeof v[s.key] === "string") v[s.key] = String(v[s.key]).toUpperCase();
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

  // ------------------------------------------------ design/option-a/admin/appearance.html

  // Extra accent swatches offered next to the theme's own default.
  const PRESETS = ["#FF7A45", "#7EE2A8", "#8FD3FF"];
  let showCss = $state(false);

  /** A theme's saved values over its defaults: draft, else published. */
  function savedValues(t: InstalledTheme): Record<string, SettingValue> {
    const base = t.draft ?? t.published;
    const out: Record<string, SettingValue> = {};
    for (const s of t.settings) out[s.key] = base?.values[s.key] ?? s.default ?? "";
    return out;
  }

  const isAccent = (s: ThemeSetting) => s.type === "color" && (s.key === "accent" || /accent|amber/.test(s.cssVar ?? ""));
  const isBackground = (s: ThemeSetting) => s.type === "color" && (s.key === "background" || s.cssVar === "--bg");

  /** The colors for a theme's thumbnail and the live preview. */
  function palette(t: InstalledTheme, v: Record<string, SettingValue>) {
    const color = (pick: (s: ThemeSetting) => boolean, fallback: string) => {
      const s = t.settings.find(pick);
      const c = s ? String(v[s.key] ?? s.default ?? "") : "";
      return /^#[0-9a-fA-F]{6}$/.test(c) ? c : fallback;
    };
    const bg = color(isBackground, "#0D1321");
    const accent = color(isAccent, "#FFB000");
    // Light text on dark backgrounds, unless the theme sets a text color.
    const dark = (contrastRatio(bg, "#000000") ?? 21) < 10;
    const text = color((s) => s.type === "color" && (s.key === "text" || s.cssVar === "--text"), dark ? "#E6E9EF" : "#1A1D23");
    return { bg, accent, text, panel: `color-mix(in srgb, ${bg} 88%, ${text})` };
  }

  const accentSetting = $derived(selected?.settings.find(isAccent) ?? null);
  const bgSetting = $derived(selected?.settings.find(isBackground) ?? null);
  const swatches = $derived.by(() => {
    const d = accentSetting && typeof accentSetting.default === "string" ? accentSetting.default.toUpperCase() : "";
    return [d, ...PRESETS].filter((c, i, all) => c && all.indexOf(c) === i);
  });
  const livePalette = $derived(selected ? palette(selected, values) : null);
  const buttonContrast = $derived(livePalette ? contrastRatio(livePalette.accent, livePalette.bg) : null);
  const otherSettings = $derived(groups.map((g) => ({ ...g, settings: g.settings.filter((s) => s !== accentSetting) })).filter((g) => g.settings.length));

  function badges(t: InstalledTheme): { text: string; kind: string }[] {
    const out: { text: string; kind: string }[] = [];
    if (t.live) out.push({ text: "Published", kind: "fill" });
    if (t.previewing) out.push({ text: "Previewing", kind: "warn" });
    if (t.scheduled) out.push({ text: "Scheduled", kind: "warn" });
    if (t.draft) out.push({ text: "Draft", kind: "" });
    if (!t.built) out.push({ text: "Not built", kind: "bad" });
    if (t.adminStyle) out.push({ text: "Styles the admin", kind: "" });
    return out;
  }

  function statusLine(t: InstalledTheme): string {
    if (t.scheduled) return `Scheduled to go live ${when(t.scheduled.publishAt)}.`;
    if (t.live && t.draft) return "Published, with unpublished changes.";
    if (t.live) return "Published.";
    if (t.draft) return "Draft. Players don't see it until you publish.";
    return "Not published.";
  }
</script>


<div class="page-head">
  <div>
    <h1>Appearance</h1>
    <p class="muted">Pick a theme, adjust it, preview it with the live event, then publish. Players switch the moment you publish.</p>
  </div>
  <div class="adm-actions">
    <!-- Theme upload waits for a later release (decision D-10). -->
    <button type="button" class="btn-line" disabled title="Theme upload comes in a later release. Bundled themes are listed below.">Upload theme (.zip)</button>
    <button type="button" class="btn-line" aria-expanded={showCss} aria-controls="custom-css" onclick={() => (showCss = !showCss)} disabled={!selected}>Custom CSS</button>
  </div>
</div>
{#if error}<p class="notice bad">{error}</p>{/if}
{#if data?.fallback}
  <p class="notice bad">The published theme can't be served (it isn't installed or isn't built), so players see the default theme. Publish a theme to fix this.</p>
{/if}
{#each data?.warnings ?? [] as w (w)}<p class="notice bad">{w}</p>{/each}

{#if data}
  <div class="row">
    <section class="grow-main" aria-labelledby="installed">
      <h2 id="installed">Installed themes <small class="muted">{data.themes.length}</small></h2>
      <div class="theme-grid">
        {#each data.themes as t (t.id)}
          {@const pal = palette(t, t.id === selectedId ? values : savedValues(t))}
          <button type="button" class="theme-card" class:on={t.id === selectedId} aria-pressed={t.id === selectedId} onclick={() => (selectedId = t.id)}>
            {#if t.screenshots[0]}
              <img class="mini" src={t.screenshots[0].url} alt="" />
            {:else}
              <span class="mini" aria-hidden="true" style="background: {pal.bg}">
                <span class="mini-top"><span style="background: {pal.accent}"></span><span style="background: {pal.text}"></span></span>
                <span class="mini-grid">
                  <span style="background: {pal.panel}; border: 1px solid {pal.accent}"></span>
                  {#each [1, 2, 3, 4] as i (i)}<span style="background: {pal.panel}"></span>{/each}
                  <span style="background: {pal.accent}"></span>
                </span>
              </span>
            {/if}
            <span class="theme-name"><b>{t.name}</b><span class="mono muted small">v{t.version}</span></span>
            <span class="badges">
              {#each badges(t) as b (b.text)}<span class="badge" class:fill={b.kind === "fill"} class:warn={b.kind === "warn"} class:bad={b.kind === "bad"}>{b.text}</span>{/each}
            </span>
          </button>
        {/each}
      </div>
    </section>

    {#if selected && livePalette}
      <form class="panel pad side-panel stack" onsubmit={save} oninput={changed} aria-labelledby="custom"
        style="--pbg: {livePalette.bg}; --ppanel: {livePalette.panel}; --ptext: {livePalette.text}; --acc: {livePalette.accent}">
        <div>
          <h2 id="custom">{selected.name}</h2>
          <p class="muted small">{statusLine(selected)} {selected.description}</p>
        </div>
        {#if !selected.built}<p class="notice bad">This theme isn't built, so it can't be previewed or published.</p>{/if}
        {#if selected.scheduled}
          <p class="notice">Goes live {when(selected.scheduled.publishAt)}. <button type="button" class="small" onclick={cancelSchedule}>Cancel</button></p>
        {/if}

        <div class="pv" aria-label="Live preview">
          <div class="pv-top">
            <span class="wordmark pv-text">{#each String(values.wordmark ?? "flag{fall}").split(/([{}])/).filter(Boolean) as part, i (i)}{#if part === "{" || part === "}"}<b class="pv-acc">{part}</b>{:else}{part}{/if}{/each}</span>
            <span class="pv-text small">Live preview</span>
          </div>
          <div class="pv-grid">
            <div class="pv-tile hl">Cookie Jar<b class="pv-acc">100</b></div>
            <div class="pv-tile">Baby RSA<b>150</b></div>
            <div class="pv-tile">Crackme v3<b>250</b></div>
          </div>
          <span class="pv-btn">Submit flag</span>
        </div>

        {#if accentSetting}
          {@const err = errors[`settings.${accentSetting.key}`] || contrastWarning(accentSetting)}
          <fieldset class="swatches">
            <legend>{accentSetting.label}</legend>
            <div class="sw-row">
              {#each swatches as c, i (c)}
                <input type="radio" name="accent" id={`sw${i}`} value={c} bind:group={values[accentSetting.key]} />
                <label for={`sw${i}`} style="background: {c}"><span class="sr-only">Accent {c}{i === 0 ? " (theme default)" : ""}</span></label>
              {/each}
              <span class="custom-color" title="Any color">
                <input type="color" aria-label="Custom accent color" bind:value={values[accentSetting.key]} />
              </span>
            </div>
            {#if buttonContrast !== null && bgSetting}
              <span class="small" class:red={buttonContrast < 4.5} class:ok-text={buttonContrast >= 4.5}>
                Button text on this accent: {(Math.floor(buttonContrast * 10) / 10).toFixed(1)}:1 ({buttonContrast >= 4.5 ? "passes" : "too low"})
              </span>
            {/if}
            {#if err}<span class="field-error">{err}</span>{/if}
          </fieldset>
        {/if}

        {#each otherSettings as g (g.name)}
          {#if otherSettings.length > 1}<h3 class="small muted group">{g.name}</h3>{/if}
          {#each g.settings as s (s.key)}
            {@const err = errors[`settings.${s.key}`] || contrastWarning(s)}
            {#if s.type === "boolean"}
              <label class="toggle-row"><span class="label">{s.label}{#if s.help}<span class="muted small">{s.help}</span>{/if}</span>
                <input type="checkbox" bind:checked={values[s.key]} /></label>
            {:else if s.type === "image"}
              <div class="field">
                <span class="label" id={`img-${s.key}`}>{s.label}</span>
                <div class="dash-box small">
                  {#if values[s.key]}
                    <img src={values[s.key]} alt={`Current ${s.label.toLowerCase()}`} />
                    <button type="button" class="small" onclick={() => { values[s.key] = ""; changed(); }}>Remove</button>
                  {:else}
                    <span class="muted">{s.key === "logo" && values.wordmark ? `Using the ${values.wordmark} wordmark` : "None"}</span>
                  {/if}
                  <label class="link">{values[s.key] ? "Replace" : "Upload"}
                    <input class="sr-only" type="file" aria-labelledby={`img-${s.key}`} accept={(s.accept ?? ["image/png", "image/svg+xml", "image/webp", "image/jpeg"]).join(",")} onchange={(ev) => uploadImage(s, ev)} />
                  </label>
                </div>
                {#if s.help}<small>{s.help}</small>{/if}
                {#if err}<span class="field-error">{err}</span>{/if}
              </div>
            {:else if (s.type === "select" || s.type === "font") && options(s).length <= 4}
              <fieldset class="seg">
                <legend>{s.label}</legend>
                <div class="choice">
                  {#each options(s) as o (o.value)}
                    <input type="radio" name={`seg-${s.key}`} id={`seg-${s.key}-${o.value}`} value={o.value} bind:group={values[s.key]} />
                    <label for={`seg-${s.key}-${o.value}`}>{o.label}</label>
                  {/each}
                </div>
                {#if s.help}<small class="muted">{s.help}</small>{/if}
              </fieldset>
            {:else}
              <label class="field">
                <span class="label">{s.label}</span>
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
                {#if s.help}<small>{s.help}</small>{/if}
                {#if err}<span class="field-error">{err}</span>{/if}
              </label>
            {/if}
          {/each}
        {/each}

        {#if showCss || customCss}
          <label class="field" id="custom-css">
            <span class="label">Custom CSS</span>
            <textarea rows="6" bind:value={customCss} spellcheck="false" placeholder=".board h1 {'{'} letter-spacing: 0.05em; {'}'}"></textarea>
            <small>Small overrides added after the theme's own styles. Only files from this server can be loaded.</small>
            {#if errors.customCss}<span class="field-error">{errors.customCss}</span>{/if}
          </label>
        {/if}

        <div class="btn-row">
          <button type="button" class="grow" onclick={preview} disabled={busy || !selected.built}>{selected.previewing ? "Open preview" : "Preview as player"}</button>
          <button type="button" class="primary grow" onclick={publish} disabled={busy || !selected.built}>{selected.live ? "Publish changes" : "Publish this theme"}</button>
        </div>
        <div class="btn-row small-row">
          <button class="small" disabled={busy}>Save draft</button>
          {#if selected.draft}<button type="button" class="small danger" onclick={discardDraft}>Discard draft</button>{/if}
          {#if selected.previewing}<button type="button" class="small" onclick={stopPreview}>Stop preview</button>{/if}
          <span class="grow"></span>
          <span class="sched">
            <input class="when" type="datetime-local" aria-label="Publish at" bind:value={scheduleAt} min={toLocalInput(new Date().toISOString())} />
            <button type="button" class="small" onclick={schedule} disabled={!scheduleAt || !selected.built}>Schedule</button>
          </span>
        </div>
        {#if dirty}<p class="hint">Unsaved changes.</p>{/if}

        <div>
          <h3 class="small muted">Publish history</h3>
          {#if history.length === 0}
            <p class="small muted">Nothing saved for this theme yet.</p>
          {:else}
            <ol class="history small">
              {#each history as v (v.version)}
                <li>
                  <b>Version {v.version} {v.status === "live" ? "published" : v.status}</b>
                  {#if v.status === "scheduled"}for {when(v.publishAt)}{:else if v.publishedAt}{v.publishedBy ? ` by ${v.publishedBy}` : ""}, {when(v.publishedAt)}{/if}
                  {#if v.status === "archived" && selected.built}<button type="button" class="link-btn" onclick={() => restore(v)}>Restore</button>{/if}
                </li>
              {/each}
            </ol>
          {/if}
        </div>
      </form>
    {/if}
  </div>
{/if}

<style>
  h2 small { font-family: var(--sans); font-weight: 400; font-size: 15px; }
  .theme-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(220px, 1fr)); gap: 14px; margin-top: 12px; }
  .theme-card { display: flex; flex-direction: column; align-items: stretch; gap: 12px; min-height: 0; padding: 12px; text-align: left; white-space: normal; font-weight: 400; background: var(--panel); border: 1px solid var(--line); border-radius: 8px; color: var(--text); }
  .theme-card:hover:not(:disabled) { color: var(--text); border-color: var(--accent-line); }
  .theme-card.on { border-color: var(--accent); box-shadow: 0 0 0 1px var(--accent); }
  .mini { display: flex; flex-direction: column; gap: 6px; height: 112px; padding: 8px; border-radius: 4px; overflow: hidden; }
  img.mini { width: 100%; object-fit: cover; padding: 0; }
  .mini-top { display: flex; justify-content: space-between; align-items: center; }
  .mini-top span:first-child { width: 44px; height: 6px; border-radius: 1px; }
  .mini-top span:last-child { width: 60px; height: 4px; border-radius: 1px; opacity: 0.4; }
  .mini-grid { flex: 1; display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 5px; }
  .mini-grid span { border-radius: 2px; outline: 1px solid rgba(255, 255, 255, 0.08); }
  .theme-name { display: flex; justify-content: space-between; align-items: baseline; gap: 8px; }

  .pv { display: flex; flex-direction: column; gap: 8px; padding: 12px; border: 1px solid var(--line); border-radius: 6px; background: var(--pbg); }
  .pv-top { display: flex; justify-content: space-between; align-items: center; }
  .pv-text { color: var(--ptext); }
  .pv-top .small { opacity: 0.7; }
  .pv-acc { color: var(--acc); }
  .wordmark { font-family: "JetBrains Mono", var(--mono); font-weight: 800; letter-spacing: -0.02em; }
  .pv-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 6px; }
  .pv-tile { display: flex; flex-direction: column; padding: 8px; background: var(--ppanel); border-radius: 3px; font-size: 12px; color: var(--ptext); outline: 1px solid rgba(255, 255, 255, 0.06); }
  .pv-tile b { font-size: 16px; }
  .pv-tile.hl { outline: 1px solid var(--acc); }
  .pv-btn { align-self: flex-start; padding: 4px 12px; border-radius: 3px; background: var(--acc); color: var(--pbg); font-size: 12px; font-weight: 700; }

  .swatches, .seg { margin: 0; padding: 0; border: 0; display: flex; flex-direction: column; gap: 8px; }
  .swatches legend, .seg legend { padding: 0; margin-bottom: 8px; font-size: 14px; font-weight: 600; color: var(--text-2); }
  .sw-row { display: flex; flex-wrap: wrap; align-items: center; gap: 10px; }
  .sw-row input[type="radio"] { position: absolute; opacity: 0; width: 1px; height: 1px; }
  .sw-row label { width: 44px; height: 44px; border-radius: 50%; border: 2px solid transparent; cursor: pointer; }
  .sw-row input:checked + label { border-color: var(--text); box-shadow: inset 0 0 0 3px var(--panel); }
  .sw-row input:focus-visible + label { outline: 2px solid var(--accent); outline-offset: 2px; }
  .custom-color input { width: 44px; height: 44px; min-height: 0; padding: 3px; border: 1px dashed var(--line-2); border-radius: 50%; background: transparent; cursor: pointer; }
  .custom-color input::-webkit-color-swatch-wrapper { padding: 0; }
  .custom-color input::-webkit-color-swatch { border: 0; border-radius: 50%; }
  .custom-color input::-moz-color-swatch { border: 0; border-radius: 50%; }
  .ok-text { color: var(--ok); }

  .choice { display: inline-flex; gap: 4px; padding: 4px; border: 1px solid var(--line); border-radius: 6px; align-self: flex-start; }
  .choice input { position: absolute; opacity: 0; width: 1px; height: 1px; }
  .choice label { padding: 6px 14px; border-radius: 4px; color: var(--text-2); cursor: pointer; font-size: 14px; }
  .choice input:checked + label { background: var(--accent); color: var(--accent-ink); font-weight: 600; }
  .choice input:focus-visible + label { outline: 2px solid var(--accent); outline-offset: 2px; }

  .dash-box { display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 10px 12px; border: 1px dashed var(--line-2); border-radius: 4px; }
  .dash-box img { width: 40px; height: 40px; object-fit: contain; background: var(--bg); border-radius: 4px; }
  .dash-box .link { cursor: pointer; }
  .color { display: flex; gap: 8px; }
  .color input[type="color"] { width: 48px; flex: none; padding: 2px; }
  .group { margin-top: 4px; }
  .toggle-row .label { display: flex; flex-direction: column; }

  .small-row { gap: 8px; }
  .sched { display: flex; gap: 6px; align-items: center; }
  .when { width: auto; min-height: 30px; padding: 2px 8px; font-size: 13px; }
  .history { margin: 6px 0 0; padding: 0; list-style: none; display: flex; flex-direction: column; gap: 4px; color: var(--muted); }
  .history b { color: var(--text); font-weight: 500; }
  .link-btn { min-height: 0; padding: 0 0 0 6px; border: 0; background: none; color: var(--accent); font-size: 13px; }
  .link-btn:hover:not(:disabled) { color: var(--accent-2); border: 0; }
</style>
