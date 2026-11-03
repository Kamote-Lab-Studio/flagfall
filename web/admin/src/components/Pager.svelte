<script lang="ts">
  let { page, pageSize, total, onPage }: { page: number; pageSize: number; total: number; onPage: (p: number) => void } = $props();
  const last = $derived(Math.max(1, Math.ceil(total / pageSize)));
</script>

{#if last > 1}
  <div class="actions">
    <button class="small" disabled={page <= 1} onclick={() => onPage(page - 1)}>← Previous</button>
    <span class="muted">Page {page} of {last} · {total} total</span>
    <button class="small" disabled={page >= last} onclick={() => onPage(page + 1)}>Next →</button>
  </div>
{:else}
  <p class="hint">{total} total</p>
{/if}
