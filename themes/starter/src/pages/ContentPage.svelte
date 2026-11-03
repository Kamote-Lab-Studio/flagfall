<script lang="ts">
  import type { Page } from "@flagfall/theme-sdk";
  import { errorText, ff } from "../lib/app.svelte";

  let { params }: { params: Record<string, string> } = $props();

  let page = $state<Page | null>(null);
  let error = $state("");

  $effect(() => {
    ff.pages.get(params.slug ?? "").then((p) => (page = p), (err) => (error = errorText(err)));
  });
</script>

{#if error}<p class="error" role="alert">{error}</p>{/if}
{#if page}
  <h1>{page.title}</h1>
  <!-- bodyHtml is sanitized by the server. -->
  <div>{@html page.bodyHtml}</div>
{/if}
