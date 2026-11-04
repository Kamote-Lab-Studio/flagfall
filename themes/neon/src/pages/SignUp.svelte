<script lang="ts">
  import { FlagfallError } from "@flagfall/theme-sdk";
  import { app, ff, refreshMe, t } from "../lib/state.svelte";
  import { navigate } from "../lib/router.svelte";
  import { errorMessage } from "../lib/util";

  let name = $state("");
  let email = $state("");
  let password = $state("");
  let country = $state("");
  let mode = $state<"create" | "join" | "later">("create");
  let teamName = $state("");
  let bracketId = $state("");
  let inviteCode = $state("");
  let busy = $state(false);
  let error = $state("");
  let fields = $state<Record<string, string>>({});

  $effect(() => {
    if (!bracketId && app.event?.brackets[0]) bracketId = app.event.brackets[0].id;
  });

  async function submit(ev: SubmitEvent) {
    ev.preventDefault();
    busy = true;
    error = "";
    fields = {};
    try {
      if (!app.me) await ff.auth.signUp({ name, email, password, country: country || undefined });
      if (mode === "create") await ff.teams.create({ name: teamName, bracketId, country: country || undefined });
      if (mode === "join") await ff.teams.join(inviteCode.trim());
      await refreshMe();
      navigate(app.me?.team ? `/teams/${app.me.team.id}` : "/", { replace: true });
    } catch (err) {
      if (err instanceof FlagfallError) fields = err.fields ?? {};
      error = errorMessage(err, t);
      await refreshMe();
    } finally {
      busy = false;
    }
  }
</script>

<main class="register">
  <h1>{app.me ? "Join a team" : "Create an account"}</h1>
  <p class="muted">Teams have up to {app.event?.maxTeamSize ?? 4} players. You can join one later from your account page.</p>

  {#if !app.event?.registrationOpen && !app.me}
    <div class="banner"><span>{t("error.registration_closed")}</span><a href="/sign-in">Sign in</a></div>
  {:else}
    <form class="stack" onsubmit={submit}>
      {#if !app.me}
        <fieldset>
          <legend>You</legend>
          <div class="field"><label for="name">Display name</label><input id="name" type="text" required maxlength="40" autocomplete="nickname" bind:value={name} aria-invalid={!!fields.name} />{#if fields.name}<small class="error">{fields.name}</small>{/if}</div>
          <div class="field"><label for="email">Email</label><input id="email" type="email" required autocomplete="email" bind:value={email} aria-invalid={!!fields.email} />{#if fields.email}<small class="error">{fields.email}</small>{/if}</div>
          <div class="field"><label for="password">Password</label><input id="password" type="password" required minlength="10" autocomplete="new-password" bind:value={password} aria-invalid={!!fields.password} /><small>{fields.password ?? "At least 10 characters."}</small></div>
          <div class="field"><label for="country">Country code <span class="muted">(optional)</span></label><input id="country" type="text" maxlength="2" placeholder="PH" bind:value={country} /></div>
        </fieldset>
      {/if}

      {#if !app.me?.team}
        <fieldset>
          <legend>Team</legend>
          <div class="choice" role="radiogroup" aria-label="Team">
            <input id="m-create" type="radio" name="mode" value="create" bind:group={mode} /><label for="m-create">Create a team</label>
            <input id="m-join" type="radio" name="mode" value="join" bind:group={mode} /><label for="m-join">Join with a code</label>
          </div>
          {#if mode === "create"}
            <div class="mode-create">
              <div class="field"><label for="team">Team name</label><input id="team" type="text" required maxlength="40" bind:value={teamName} aria-invalid={!!fields.name} /></div>
              {#if (app.event?.brackets.length ?? 0) > 1}
                <div class="field"><span class="label">Bracket</span>
                  {#each app.event?.brackets ?? [] as b}
                    <label class="radio"><input type="radio" name="bracket" value={b.id} bind:group={bracketId} /><span>{b.name}<small>{b.description}</small></span></label>
                  {/each}
                </div>
              {/if}
            </div>
          {:else if mode === "join"}
            <div class="field" style="display:flex"><label for="code">Invite code</label><input id="code" class="code-input" type="text" required autocomplete="off" bind:value={inviteCode} /><small>Ask your captain for it. It's on their team page.</small></div>
          {/if}
          <button type="button" class="link" onclick={() => (mode = "later")}>Skip for now</button>
        </fieldset>
      {/if}

      {#if error}<p class="status-msg bad" role="alert">{error}</p>{/if}
      <button class="btn" type="submit" disabled={busy}>{app.me ? "Continue" : "Create account"}</button>
      {#if !app.me}<p class="muted">Already registered? <a href="/sign-in">Sign in</a></p>{/if}
    </form>
  {/if}
</main>
