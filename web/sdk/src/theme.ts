import type { SettingValues, ThemeContext } from "./index.js";
import { defaults } from "./messages.js";

export interface ThemeInfo {
  id: string;
  version: number;
  settings: SettingValues;
  preview: boolean;
  locale: string;
  /** Organizer wording overrides for the current locale. */
  messages: Record<string, string>;
}

/**
 * The admin panel posts on this same-origin channel when the organizer saves
 * a draft or ends preview, so open preview tabs show the change. Server-side
 * values (CSS variables, custom CSS) only change on a page load, so preview
 * tabs reload.
 */
export const PREVIEW_CHANNEL = "flagfall-theme-preview";

export interface PreviewMessage {
  type: "draft-saved" | "preview-ended" | "published";
  themeId: string;
}

interface ChannelLike {
  onmessage: ((ev: { data: unknown }) => void) | null;
}

export interface ThemeContextOptions {
  /** For tests: the preview channel and what "reload" does. */
  channel?: ChannelLike | null;
  reload?: () => void;
}

export function createThemeContext(info: ThemeInfo, opts: ThemeContextOptions = {}): ThemeContext {
  const themeMessages = new Map<string, Record<string, string>>();
  const settingsHandlers = new Set<(s: SettingValues) => void>();
  const locale = info.locale || "en";

  if (info.preview) {
    const channel =
      opts.channel !== undefined
        ? opts.channel
        : typeof BroadcastChannel !== "undefined"
          ? (new BroadcastChannel(PREVIEW_CHANNEL) as unknown as ChannelLike)
          : null;
    const reload = opts.reload ?? (() => typeof location !== "undefined" && location.reload());
    if (channel) {
      channel.onmessage = (ev) => {
        const m = ev.data as Partial<PreviewMessage> | null;
        if (!m || typeof m !== "object") return;
        // A saved draft only matters for the theme being previewed; ending
        // preview or publishing changes what this tab should show anyway.
        if (m.type === "draft-saved" && m.themeId !== info.id) return;
        if (m.type === "draft-saved" || m.type === "preview-ended" || m.type === "published") reload();
      };
    }
  }

  const lookup = (key: string): string | undefined =>
    info.messages[key] ??
    themeMessages.get(locale)?.[key] ??
    themeMessages.get("en")?.[key] ??
    defaults[locale]?.[key] ??
    defaults.en?.[key];

  return {
    id: info.id,
    settings: info.settings ?? {},
    preview: info.preview,
    locale,
    t(key, vars) {
      const template = lookup(key) ?? key;
      if (!vars) return template;
      return template.replace(/\{(\w+)\}/g, (m, name: string) => (name in vars ? String(vars[name]) : m));
    },
    addMessages(loc, messages) {
      themeMessages.set(loc, { ...themeMessages.get(loc), ...messages });
    },
    onSettingsChange(handler) {
      settingsHandlers.add(handler);
      return () => settingsHandlers.delete(handler);
    },
  };
}
