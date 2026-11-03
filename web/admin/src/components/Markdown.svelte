<script lang="ts">
  import { api, message } from "../lib/api";

  let { label, value = $bindable(""), rows = 10 }: { label: string; value?: string; rows?: number } = $props();

  let preview = $state(false);
  let html = $state("");
  let error = $state("");

  async function toggle() {
    preview = !preview;
    if (!preview) return;
    try {
      // Rendered by the server exactly as players will see it.
      html = (await api.post<{ html: string }>("/preview", { markdown: value })).html;
      error = "";
    } catch (err) {
      error = message(err);
    }
  }
</script>

<div class="md">
  <div class="head">
    <span class="label">{label}</span>
    <button type="button" class="small" onclick={toggle} aria-pressed={preview}>{preview ? "Edit" : "Preview"}</button>
  </div>
  {#if preview}
    {#if error}<p class="field-error">{error}</p>{/if}
    <!-- Sanitized server-side by the same renderer players get. -->
    <div class="rendered">{@html html}</div>
  {:else}
    <textarea aria-label={label} {rows} bind:value></textarea>
  {/if}
</div>

<style>
  .md { margin-bottom: 0.85rem; }
  .head { display: flex; justify-content: space-between; align-items: end; margin-bottom: 0.25rem; }
  .label { color: var(--muted); font-size: 0.85rem; }
</style>
