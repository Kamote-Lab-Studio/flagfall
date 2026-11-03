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

export function createThemeContext(info: ThemeInfo): ThemeContext {
  const themeMessages = new Map<string, Record<string, string>>();
  const settingsHandlers = new Set<(s: SettingValues) => void>();
  const locale = info.locale || "en";

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
