<script lang="ts">
  import { relativeTime, type Announcement } from "@flagfall/theme-sdk";
  import { app, ff, t } from "../lib/state.svelte";
  import { formatDate } from "../lib/util";

  let items = $state<Announcement[] | null>(null);

  $effect(() => {
    void app.announcementVersion;
    ff.announcements.list().then((a) => (items = a));
  });
</script>

<main class="narrow stack">
  <h1>{t("nav.announcements")}</h1>
  {#if !items}
    <p class="loading">Loading…</p>
  {:else if items.length === 0}
    <p class="muted">Nothing posted yet. Announcements show up here and as a notice while you play.</p>
  {:else}
    <div class="ann-list">
      {#each items as a (a.id)}
        <article class="ann" class:pinned={a.pinned}>
          <div class="ann-meta">{#if a.pinned}<span class="tag">Pinned</span>{/if}<time datetime={a.postedAt} title={formatDate(a.postedAt)}>{relativeTime(a.postedAt, t)}</time></div>
          <h2>{a.title}</h2>
          <div class="desc">{@html a.bodyHtml}</div>
        </article>
      {/each}
    </div>
  {/if}
</main>
