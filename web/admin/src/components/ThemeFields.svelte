<script lang="ts">
  import type { ChallengeField } from "../lib/types";

  let {
    fields,
    themeName,
    values = $bindable(),
    errors,
  }: {
    fields: ChallengeField[];
    themeName: string;
    // Per-field input state from lib/themefields.ts (toInputs). Loosely typed
    // because Svelte can't bind through a type cast.
    values: Record<string, any>;
    errors: Record<string, string>;
  } = $props();

  function err(key: string): string | undefined {
    return errors[`themeFields.${key}`];
  }
</script>

<fieldset class="theme-fields">
  <legend>Fields for the {themeName} theme</legend>
  <div class="grid">
    {#each fields as f (f.key)}
      {#if f.type === "boolean"}
        <label class="check"><input type="checkbox" bind:checked={values[f.key]} /><span>{f.label}</span></label>
      {:else if f.type === "location"}
        <div class="location">
          <span>{f.label}{#if f.required}<span class="req">(required)</span>{/if}</span>
          <div class="row">
            <input aria-label={`${f.label}: city`} placeholder="City (optional)" bind:value={values[f.key].city} />
            <input aria-label={`${f.label}: latitude`} placeholder="Latitude" inputmode="decimal" bind:value={values[f.key].lat} />
            <input aria-label={`${f.label}: longitude`} placeholder="Longitude" inputmode="decimal" bind:value={values[f.key].lon} />
          </div>
          {#if f.help}<div class="hint">{f.help}</div>{/if}
          {#if err(f.key)}<div class="field-error">{err(f.key)}</div>{/if}
        </div>
      {:else}
        <label>
          <span>{f.label}{#if f.required}<span class="req">(required)</span>{/if}</span>
          {#if f.type === "select"}
            <select bind:value={values[f.key]}>
              <option value="">—</option>
              {#each f.options ?? [] as o (o.value)}<option value={o.value}>{o.label}</option>{/each}
            </select>
          {:else}
            <input inputmode={f.type === "number" ? "decimal" : undefined} bind:value={values[f.key]} />
          {/if}
          {#if f.help}<div class="hint">{f.help}</div>{/if}
          {#if err(f.key)}<div class="field-error">{err(f.key)}</div>{/if}
        </label>
      {/if}
    {/each}
  </div>
</fieldset>

<style>
  .theme-fields { border: 1px solid var(--line, #333); border-radius: var(--radius); padding: 0.5rem 0.9rem 0.9rem; margin: 0.75rem 0; }
  legend { padding: 0 0.4rem; color: var(--muted); }
  .location { grid-column: 1 / -1; }
  .location .row { display: grid; grid-template-columns: 2fr 1fr 1fr; gap: 0.5rem; max-width: 36rem; }
  .req { color: var(--muted); margin-left: 0.35rem; }
</style>
