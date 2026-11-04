<script lang="ts">
  import { setting } from "../lib/state.svelte";
  import Wordmark from "./Wordmark.svelte";

  const logo = $derived(setting("logo"));
</script>

<a class="logo" href="/">
  {#if logo}
    <img class="logo-img" src={logo} alt="" width="44" height="44" />
  {:else}
    <svg aria-hidden="true" width="44" height="44" viewBox="0 0 64 64"><path d="M16 8c-5 0-7 2-7 7v9c0 4-2 6-5 8 3 2 5 4 5 8v9c0 5 2 7 7 7" fill="none" stroke="var(--amber)" stroke-width="6" stroke-linecap="round" stroke-linejoin="round"/><path d="M48 8c5 0 7 2 7 7v9c0 4 2 6 5 8-3 2-5 4-5 8v9c0 5-2 7-7 7" fill="none" stroke="var(--amber)" stroke-width="6" stroke-linecap="round" stroke-linejoin="round"/><path d="M24 18h5v30h-5ZM29 21 L45 28 L29 35Z" fill="var(--green)"/></svg>
  {/if}
  <Wordmark />
</a>
