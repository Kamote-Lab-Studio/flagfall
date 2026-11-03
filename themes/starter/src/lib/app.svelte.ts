// Everything the pages share: the SDK client, the signed-in player, the
// event, settings, and a tiny router. Start reading here.

import { createClient, isError, type Event, type Me, type SettingValues, type ThemeContext } from "@flagfall/theme-sdk";
import manifest from "../../theme.json";

/** The SDK client. It only talks to the player API (/api/v1). */
export const ff = createClient();

/** Setting defaults from theme.json; published values are merged over them. */
const defaults: SettingValues = Object.fromEntries(
  manifest.settings.map((s) => [s.key, "default" in s ? (s.default as string) : null]),
);

export const app = $state({
  ready: false,
  event: null as Event | null,
  me: null as Me | null,
  theme: null as ThemeContext | null,
  settings: { ...defaults } as SettingValues,
  /** Bumped by live events, so pages know to fetch again. */
  version: { scoreboard: 0, challenges: 0, announcements: 0 },
});

/** A setting as a string ("" when unset). */
export function setting(key: string): string {
  const v = app.settings[key];
  return v == null ? "" : String(v);
}

/** Translate a message key (organizer wording first, then SDK defaults). */
export function t(key: string, vars?: Record<string, string | number>): string {
  return app.theme ? app.theme.t(key, vars) : key;
}

/** A readable message for any error the SDK throws. */
export function errorText(err: unknown): string {
  if (isError(err)) return err.message;
  return "Something went wrong. Try again.";
}

export async function refreshEvent() {
  app.event = await ff.event.get();
}

export async function refreshMe() {
  app.me = await ff.auth.me();
}

/** Loads the theme, event and player, and subscribes to live updates. */
export async function start() {
  const [theme] = await Promise.all([ff.loadTheme(), refreshEvent(), refreshMe()]);
  app.theme = theme;
  app.settings = { ...defaults, ...theme.settings };
  document.documentElement.lang = theme.locale;
  // The server already put cssVar settings on :root; nothing to do for colors.

  ff.live.on("scoreboard", () => app.version.scoreboard++);
  ff.live.on("solve", () => app.version.challenges++);
  ff.live.on("challenge-updated", () => app.version.challenges++);
  ff.live.on("announcement", () => app.version.announcements++);
  ff.live.on("event-state", () => refreshEvent());
  ff.live.on("resync", () => {
    refreshEvent();
    refreshMe();
    app.version.scoreboard++;
    app.version.challenges++;
    app.version.announcements++;
  });
}

// ---------------------------------------------------------------- router

export const route = $state({ path: location.pathname, query: new URLSearchParams(location.search) });

function update() {
  route.path = location.pathname;
  route.query = new URLSearchParams(location.search);
}

export function navigate(to: string) {
  history.pushState(null, "", to);
  update();
  window.scrollTo(0, 0);
}

/** Match "/teams/:id" against a path; returns params or null. */
export function match(pattern: string, path: string): Record<string, string> | null {
  const p = pattern.split("/").filter(Boolean);
  const s = path.split("/").filter(Boolean);
  if (p.length !== s.length) return null;
  const params: Record<string, string> = {};
  for (let i = 0; i < p.length; i++) {
    const part = p[i]!;
    if (part.startsWith(":")) params[part.slice(1)] = decodeURIComponent(s[i]!);
    else if (part !== s[i]) return null;
  }
  return params;
}

/** Same-origin link clicks change the page without a reload. */
export function installLinks() {
  window.addEventListener("popstate", update);
  document.addEventListener("click", (ev) => {
    if (ev.defaultPrevented || ev.button !== 0 || ev.metaKey || ev.ctrlKey || ev.shiftKey || ev.altKey) return;
    const a = (ev.target as Element | null)?.closest?.("a");
    if (!a || a.target || a.hasAttribute("download")) return;
    const url = new URL(a.href, location.href);
    // Downloads, the API, theme files and the admin panel are real pages.
    const real = ["/api/", "/files/", "/theme/", "/theme-assets/", "/admin"];
    if (url.origin !== location.origin || real.some((p) => url.pathname.startsWith(p))) return;
    ev.preventDefault();
    navigate(url.pathname + url.search);
  });
}
