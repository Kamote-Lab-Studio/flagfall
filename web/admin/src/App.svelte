<script lang="ts">
  import { onMount } from "svelte";
  import { api, ApiError, message } from "./lib/api";
  import { installLinkHandler } from "./lib/router.svelte";
  import { session, toast } from "./lib/state.svelte";
  import type { Me } from "./lib/types";
  import Setup from "./pages/Setup.svelte";
  import SignIn from "./pages/SignIn.svelte";
  import Shell from "./components/Shell.svelte";

  type View = "loading" | "setup" | "sign-in" | "not-staff" | "ready" | "error";
  let view = $state<View>("loading");
  let error = $state("");
  let demo = $state<{ email: string; password: string } | null>(null);

  async function load() {
    try {
      const setup = await api.get<{ required: boolean; demoAccount: { email: string; password: string } | null }>("/setup");
      demo = setup.demoAccount;
      if (setup.required) {
        view = "setup";
        return;
      }
      session.me = await api.get<Me>("/me");
      view = "ready";
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) view = "sign-in";
      else if (err instanceof ApiError && err.status === 403) view = "not-staff";
      else {
        error = message(err);
        view = "error";
      }
    }
  }

  async function signOut() {
    await api.signOut().catch(() => {});
    session.me = null;
    view = "sign-in";
  }

  onMount(() => {
    installLinkHandler();
    load();
  });
</script>

{#if view === "loading"}
  <p class="center muted">Loading…</p>
{:else if view === "setup"}
  <Setup onDone={load} />
{:else if view === "sign-in"}
  <SignIn onDone={load} {demo} />
{:else if view === "not-staff"}
  <main class="center">
    <h1>Staff only</h1>
    <p>You're signed in, but your account isn't a staff account. Ask an event owner to give you a role.</p>
    <div class="actions"><a class="button" href="/">Back to the event</a><button onclick={signOut}>Sign out</button></div>
  </main>
{:else if view === "error"}
  <main class="center">
    <h1>Can't reach the server</h1>
    <p>{error}</p>
    <button onclick={load}>Try again</button>
  </main>
{:else}
  <Shell onSignOut={signOut} />
{/if}

{#if toast.text}
  <div class="toast" class:error={toast.kind === "error"} role="status">{toast.text}</div>
{/if}

<style>
  .center { max-width: 30rem; margin: 15vh auto; padding: 0 1rem; }
  .toast {
    position: fixed; bottom: 1.25rem; left: 50%; transform: translateX(-50%);
    background: var(--panel-2); border: 1px solid var(--ok); color: var(--text);
    padding: 0.5rem 1rem; border-radius: var(--radius); box-shadow: 0 6px 24px #0008; z-index: 10;
  }
  .toast.error { border-color: var(--bad); }
</style>
