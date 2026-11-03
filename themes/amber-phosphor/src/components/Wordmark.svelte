<script lang="ts">
  import { setting } from "../lib/state.svelte";

  // Braces are drawn in the accent color, as in the flag{fall} logo.
  const parts = $derived(setting("wordmark").split(/([{}])/).filter(Boolean));
</script>

<span class="wordmark">{#each parts as p}{#if p === "{" || p === "}"}<b>{p}</b>{:else}{p}{/if}{/each}</span>
