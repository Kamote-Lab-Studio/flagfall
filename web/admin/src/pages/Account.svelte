<script lang="ts">
  import { onMount } from "svelte";
  import { api, fieldErrors, message } from "../lib/api";
  import { notify, session } from "../lib/state.svelte";

  // The signed-in staff member's own two-factor sign-in (TOTP).
  type Status = { enabled: boolean; recoveryCodesLeft: number };
  type Setup = { secret: string; url: string; qrSvg: string };

  let status = $state<Status | null>(null);
  let setup = $state<Setup | null>(null);
  let codes = $state<string[]>([]);
  let code = $state("");
  let password = $state("");
  let errors = $state<Record<string, string>>({});
  let error = $state("");
  let busy = $state(false);

  async function load() {
    try {
      status = await api.me.get<Status>("/two-factor");
      error = "";
    } catch (err) {
      error = message(err);
    }
  }

  async function run(fn: () => Promise<void>) {
    busy = true;
    errors = {};
    try {
      await fn();
    } catch (err) {
      errors = fieldErrors(err);
      if (!Object.keys(errors).length) notify(message(err), "error");
    } finally {
      busy = false;
    }
  }

  const start = () => run(async () => {
    setup = await api.me.post<Setup>("/two-factor/setup");
    code = "";
  });

  const enable = (ev: SubmitEvent) => {
    ev.preventDefault();
    return run(async () => {
      codes = (await api.me.post<{ recoveryCodes: string[] }>("/two-factor/enable", { code })).recoveryCodes;
      setup = null;
      await load();
    });
  };

  const newCodes = (ev: SubmitEvent) => {
    ev.preventDefault();
    return run(async () => {
      codes = (await api.me.post<{ recoveryCodes: string[] }>("/two-factor/recovery-codes", { password })).recoveryCodes;
      password = "";
      await load();
    });
  };

  const disable = () => {
    if (!confirm("Turn off two-factor sign-in? Your password alone will sign you in.")) return;
    return run(async () => {
      await api.me.post("/two-factor/disable", { password });
      password = "";
      codes = [];
      notify("Two-factor sign-in is off.");
      await load();
    });
  };

  function download() {
    const text = `Flagfall recovery codes for ${session.me?.email ?? "your account"}\nEach works once, in place of a code from your app.\n\n${codes.join("\n")}\n`;
    const a = document.createElement("a");
    a.href = URL.createObjectURL(new Blob([text], { type: "text/plain" }));
    a.download = "flagfall-recovery-codes.txt";
    a.click();
    URL.revokeObjectURL(a.href);
  }

  onMount(load);
</script>

<div class="page-head">
  <div>
    <h1>Account security</h1>
    <p class="muted">Signed in as {session.me?.name} ({session.me?.email}).</p>
  </div>
</div>
{#if error}<p class="notice bad">{error}</p>{/if}

<section class="panel pad stack" aria-labelledby="tfa-title">
  <div class="panel-head">
    <h2 id="tfa-title">Two-factor sign-in</h2>
    {#if status}<span class="badge" class:ok={status.enabled}>{status.enabled ? "On" : "Off"}</span>{/if}
  </div>
  <p class="muted">After your password, sign-in asks for a code from an authenticator app on your phone, so a leaked password alone can't get into the admin panel.</p>

  {#if codes.length}
    <div class="notice stack-s" role="status">
      <b>Your recovery codes</b>
      <p class="small">Each one works once, in place of a code from your app, if you lose your phone. Keep them somewhere safe: they aren't shown again.</p>
      <ol class="codes mono">{#each codes as c (c)}<li>{c}</li>{/each}</ol>
      <div class="btn-row">
        <button type="button" onclick={download}>Download as a text file</button>
        <button type="button" class="link" onclick={() => (codes = [])}>I've saved them</button>
      </div>
    </div>
  {/if}

  {#if !status}
    <p class="muted">Loading…</p>
  {:else if status.enabled}
    <p>{status.recoveryCodesLeft} of 10 recovery codes left.</p>
    <form class="stack-s" onsubmit={newCodes}>
      <label>
        <span>Your password, to make changes</span>
        <input type="password" autocomplete="current-password" bind:value={password} required />
      </label>
      {#if errors.password}<p class="field-error">{errors.password}</p>{/if}
      <div class="btn-row">
        <button disabled={busy}>Make new recovery codes</button>
        <button type="button" class="danger" disabled={busy || !password} onclick={disable}>Turn off</button>
      </div>
    </form>
  {:else if setup}
    <ol class="steps">
      <li>Scan this with an authenticator app (any that supports TOTP, such as Aegis, 2FAS, Google Authenticator or 1Password).
        <div class="qr">{@html setup.qrSvg}</div>
        <p class="small muted">Can't scan it? Enter this key: <code class="mono">{setup.secret}</code></p>
      </li>
      <li>
        <form class="stack-s" onsubmit={enable}>
          <label>
            <span>Then enter the 6-digit code it shows</span>
            <input class="mono" style="max-width:12rem" inputmode="numeric" autocomplete="one-time-code" maxlength="7" bind:value={code} required />
          </label>
          {#if errors.code}<p class="field-error">{errors.code}</p>{/if}
          <div class="btn-row">
            <button class="primary" disabled={busy}>Turn on</button>
            <button type="button" class="link" onclick={() => (setup = null)}>Cancel</button>
          </div>
        </form>
      </li>
    </ol>
  {:else}
    <div><button class="primary" onclick={start} disabled={busy}>Set up two-factor sign-in</button></div>
  {/if}
</section>

<style>
  .qr { width: 200px; margin: 12px 0 6px; padding: 8px; background: #fff; border-radius: 6px; }
  .qr :global(svg) { display: block; width: 100%; height: auto; }
  .steps { margin: 0; padding-left: 1.2rem; display: flex; flex-direction: column; gap: 16px; }
  .codes { display: grid; grid-template-columns: repeat(2, max-content); gap: 4px 32px; margin: 0; padding-left: 1.4rem; }
</style>
