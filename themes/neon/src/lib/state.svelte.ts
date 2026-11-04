import { createClient, type Event, type Me, type SettingValues, type ThemeContext } from "@flagfall/theme-sdk";
import manifest from "../../theme.json";

export const ff = createClient();

/** Defaults from theme.json, overridden by what the organizer published. */
const defaults: SettingValues = Object.fromEntries(
  manifest.settings.map((s) => [s.key, "default" in s ? (s.default as string) : null]),
);

export interface Toast {
  id: number;
  title: string;
  body?: string;
  bad?: boolean;
}

export const app = $state({
  ready: false,
  event: null as Event | null,
  me: null as Me | null,
  theme: null as ThemeContext | null,
  settings: { ...defaults } as SettingValues,
  /** Server time minus local time, so countdowns are right on skewed clocks. */
  clockOffset: 0,
  now: Date.now(),
  /** Bumped by live events; pages re-fetch when these change. */
  scoreVersion: 0,
  challengeVersion: 0,
  announcementVersion: 0,
  liveOpen: false,
  toasts: [] as Toast[],
});

export function t(key: string, vars?: Record<string, string | number>): string {
  return app.theme ? app.theme.t(key, vars) : key;
}

export function setting(key: string): string {
  const v = app.settings[key];
  return v == null ? "" : String(v);
}

let toastId = 0;
export function toast(title: string, body?: string, bad = false) {
  const id = ++toastId;
  app.toasts.push({ id, title, body, bad });
  setTimeout(() => {
    app.toasts = app.toasts.filter((x) => x.id !== id);
  }, 6000);
}

export async function refreshEvent() {
  const ev = await ff.event.get();
  app.event = ev;
  app.clockOffset = new Date(ev.serverTime).getTime() - Date.now();
}

export async function refreshMe() {
  app.me = await ff.auth.me();
}

export function serverNow(): number {
  return app.now + app.clockOffset;
}

export function applySettings(settings: SettingValues) {
  app.settings = { ...defaults, ...settings };
  const root = document.documentElement;
  for (const s of manifest.settings) {
    const v = app.settings[s.key];
    if ("cssVar" in s && s.cssVar && typeof v === "string" && v) root.style.setProperty(s.cssVar, v);
  }
  // An uploaded favicon replaces the bundled one.
  const icon = setting("favicon");
  const link = document.querySelector<HTMLLinkElement>("link[rel=icon]");
  if (link && icon) {
    link.href = icon;
    link.removeAttribute("type");
  }
}
