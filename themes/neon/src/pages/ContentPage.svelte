<script lang="ts">
  import type { Page } from "@flagfall/theme-sdk";
  import { ff } from "../lib/state.svelte";

  let { params }: { params: { slug?: string } } = $props();

  let page = $state<Page | null>(null);
  let notFound = $state(false);
  let article = $state<HTMLElement | null>(null);
  let toc = $state<{ id: string; text: string }[]>([]);

  $effect(() => {
    const slug = params.slug ?? "";
    ff.pages
      .get(slug)
      .then((p) => {
        page = p;
        notFound = false;
      })
      .catch(() => {
        page = null;
        notFound = true;
      });
  });

  // Build the table of contents from the rendered h2 headings.
  $effect(() => {
    void page;
    if (!article) return;
    const used = new Set<string>();
    toc = [...article.querySelectorAll("h2")].map((h) => {
      let id = h.id || (h.textContent ?? "").toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "") || "section";
      while (used.has(id)) id += "-";
      used.add(id);
      h.id = id;
      return { id, text: h.textContent ?? "" };
    });
  });
</script>

{#if notFound}
  <main class="narrow"><h1>Page not found</h1><p class="muted"><a href="/">Back to the challenges</a></p></main>
{:else if !page}
  <p class="loading">Loading…</p>
{:else}
  <main class="rules">
    {#if toc.length > 1}
      <nav class="toc" aria-label="On this page">
        <p class="muted">On this page</p>
        <ul>{#each toc as item}<li><a href="#{item.id}">{item.text}</a></li>{/each}</ul>
      </nav>
    {/if}
    <article bind:this={article}>
      <h1>{page.title}</h1>
      <div class="desc">{@html page.bodyHtml}</div>
    </article>
  </main>
{/if}
