<script lang="ts">
  import { app } from "../lib/app.svelte";

  let { params: _ }: { params: Record<string, string> } = $props();
</script>

<h1>{app.event?.name ?? "The event"} is over</h1>
<p>Thanks for playing. See the <a href="/scoreboard">final scoreboard</a>.</p>
{#if app.me?.team}<p>Your team finished with {app.me.team.score} points.</p>{/if}
