<script lang="ts">
  import { formatDuration } from "@flagfall/theme-sdk";
  import { app, serverNow } from "../lib/state.svelte";
  import { formatDate } from "../lib/util";

  const left = $derived(app.event?.startsAt ? new Date(app.event.startsAt).getTime() - serverNow() : 0);
</script>

<main class="state-screen">
  <pre class="term">$ ./{(app.event?.name ?? "event").toLowerCase().replace(/\s+/g, "-")} --start
waiting for the clock…</pre>
  <h1 class="term-big">Starting soon<span class="cursor" aria-hidden="true"></span></h1>
  <p class="countdown" role="timer" aria-live="off">{formatDuration(Math.max(0, left))}</p>
  <p class="muted">Challenges open {formatDate(app.event?.startsAt)}. Use the time to sign up, form a team and read the rules.</p>
  <div class="row-actions">
    {#if !app.me}<a class="btn" href="/sign-up">Create an account</a>{:else if !app.me.team}<a class="btn" href="/account">Create or join a team</a>{/if}
    <a class="btn-ghost" href="/p/rules">Read the rules</a>
  </div>
</main>
