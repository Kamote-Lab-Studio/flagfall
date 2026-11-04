<script lang="ts">
  let { name, size = 16 }: { name: "check" | "clock" | "download"; size?: number } = $props();
</script>

<svg aria-hidden="true" width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width={name === "check" ? 2.5 : 2} stroke-linecap="round" stroke-linejoin="round">
  {#if name === "check"}<path d="M5 12l5 5L20 7" />
  {:else if name === "clock"}<circle cx="12" cy="12" r="9" /><path d="M12 7v5l3 2" />
  {:else}<path d="M12 4v12m0 0l-5-5m5 5l5-5M5 20h14" />{/if}
</svg>
