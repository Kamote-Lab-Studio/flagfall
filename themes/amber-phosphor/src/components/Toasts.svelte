<script lang="ts">
  import { app } from "../lib/state.svelte";
</script>

<div class="toasts" role="status" aria-live="polite">
  {#each app.toasts as toast (toast.id)}
    <div class="toast" class:bad={toast.bad}>
      <b>{toast.title}</b>
      {#if toast.body}<span>{toast.body}</span>{/if}
    </div>
  {/each}
</div>
