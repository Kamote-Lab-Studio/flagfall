/**
 * @flagfall/theme-sdk: everything a theme needs to talk to Flagfall.
 *
 * Rules the SDK enforces for themes:
 *  - It only talks to the player API (/api/v1). It has no admin functions,
 *    and the server rejects player sessions on /admin routes anyway.
 *  - Errors are thrown as FlagfallError with a stable `code`.
 *  - Everything a theme shows that can change during an event (scoreboard,
 *    solves, announcements, event state, theme) is available live.
 *
 * See docs/dev/sdk.md.
 */

import type {
  Announcement,
  ApiErrorBody,
  Challenge,
  ChallengeSummary,
  ErrorCode,
  Event,
  FlagResult,
  ID,
  LiveEventData,
  LiveEventType,
  Me,
  MyTeam,
  Page,
  Scoreboard,
  ScoreSeries,
  Team,
  TeamSummary,
} from "./types.js";
import { createLive } from "./live.js";
import { createThemeContext, type ThemeInfo } from "./theme.js";
export { PREVIEW_CHANNEL, type PreviewMessage } from "./theme.js";

export type * from "./types.js";
export { relativeTime, formatDuration } from "./format.js";

// ---------------------------------------------------------------- Client

export interface ClientOptions {
  /** Defaults to the site the theme is served from. */
  baseUrl?: string;
  /** Override fetch, for tests. */
  fetch?: typeof fetch;
  /** Override EventSource, for tests. */
  EventSource?: typeof EventSource;
}

export interface Paged<T> {
  items: T[];
  total: number;
  page: number;
  pageSize: number;
}

export interface Flagfall {
  event: {
    get(): Promise<Event>;
  };

  auth: {
    /** Null when signed out. */
    me(): Promise<Me | null>;
    signIn(input: { email: string; password: string }): Promise<Me>;
    signUp(input: { name: string; email: string; password: string; country?: string }): Promise<Me>;
    signOut(): Promise<void>;
    requestPasswordReset(email: string): Promise<void>;
    resetPassword(input: { token: string; password: string }): Promise<void>;
    updateAccount(input: { name?: string; country?: string | null }): Promise<Me>;
    changePassword(input: { current: string; next: string }): Promise<void>;
  };

  teams: {
    list(query?: { bracketId?: ID; search?: string; recruiting?: boolean; page?: number; pageSize?: number }): Promise<Paged<TeamSummary>>;
    get(id: ID): Promise<Team | MyTeam>;
    create(input: { name: string; country?: string; bracketId: ID }): Promise<MyTeam>;
    join(inviteCode: string): Promise<MyTeam>;
    leave(): Promise<void>;
    /** Captain only. */
    update(input: { name?: string; country?: string | null; recruiting?: boolean }): Promise<MyTeam>;
    /** Captain only. Invalidates the old code. */
    regenerateInviteCode(): Promise<MyTeam>;
    /** Captain only. */
    removeMember(userId: ID): Promise<MyTeam>;
    /** Captain only. */
    transferCaptain(userId: ID): Promise<MyTeam>;
  };

  challenges: {
    list(): Promise<ChallengeSummary[]>;
    get(slug: string): Promise<Challenge>;
    /** Never throws for wrong or rate-limited flags; check `status`. */
    submitFlag(slug: string, flag: string): Promise<FlagResult>;
    /** Spends the hint's cost from the team's score and returns the content. */
    unlockHint(slug: string, hintId: ID): Promise<{ id: ID; content: string }>;
  };

  scoreboard: {
    get(query?: { bracketId?: ID }): Promise<Scoreboard>;
    /** Score-over-time lines for the top N teams (default 10, max 25). */
    series(query?: { bracketId?: ID; top?: number; teamId?: ID }): Promise<ScoreSeries[]>;
  };

  announcements: {
    list(): Promise<Announcement[]>;
  };

  pages: {
    get(slug: string): Promise<Page>;
    list(): Promise<Pick<Page, "slug" | "title">[]>;
  };

  live: LiveConnection;

  /** Loads theme settings and messages. Call once before rendering. */
  loadTheme(): Promise<ThemeContext>;
}

// ---------------------------------------------------------------- Live updates

export type LiveStatus = "connecting" | "open" | "reconnecting" | "closed";

export interface LiveConnection {
  /**
   * Listen for one type of live event. Returns a function that removes the
   * listener. The connection opens on the first subscription and reconnects
   * on its own.
   *
   * "theme-published" is handled by the SDK by default (it reloads the page);
   * subscribing replaces that behaviour.
   */
  on<T extends LiveEventType | "resync">(type: T, handler: (data: T extends LiveEventType ? LiveEventData<T> : Record<string, never>) => void): () => void;
  readonly status: LiveStatus;
  onStatus(handler: (status: LiveStatus) => void): () => void;
  close(): void;
}

// ---------------------------------------------------------------- Theme context

/** Setting values, keyed by the names in the theme's theme.json settings schema. */
export type SettingValues = Record<string, string | number | boolean | null>;

export interface ThemeContext {
  readonly id: string;
  /** Published settings; empty until an organizer customizes the theme. Merge over your own defaults. */
  readonly settings: SettingValues;
  /** True in admin preview mode; themes should keep the preview banner visible. */
  readonly preview: boolean;
  readonly locale: string;
  /**
   * Translate a message key. Looks up the organizer's wording overrides first,
   * then the theme's messages, then the SDK defaults; falls back to the key.
   */
  t(key: string, vars?: Record<string, string | number>): string;
  /** Register the theme's own messages for a locale. */
  addMessages(locale: string, messages: Record<string, string>): void;
  /** Fires when settings change during preview. */
  onSettingsChange(handler: (settings: SettingValues) => void): () => void;
}

// ---------------------------------------------------------------- Errors

export class FlagfallError extends Error {
  readonly code: ErrorCode;
  readonly status: number;
  readonly fields: Record<string, string>;

  constructor(status: number, body: ApiErrorBody | null) {
    super(body?.error.message ?? `Request failed (${status})`);
    this.name = "FlagfallError";
    this.status = status;
    this.code = body?.error.code ?? (status >= 500 ? "internal" : "validation");
    this.fields = body?.error.fields ?? {};
  }
}

// ---------------------------------------------------------------- Implementation

export function createClient(options: ClientOptions = {}): Flagfall {
  const base = (options.baseUrl ?? "").replace(/\/$/, "");
  const doFetch = options.fetch ?? ((...a: Parameters<typeof fetch>) => fetch(...a));

  async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
    const init: RequestInit = { method, credentials: "same-origin", headers: { Accept: "application/json" } };
    if (body !== undefined) {
      init.body = JSON.stringify(body);
      init.headers = { ...init.headers, "Content-Type": "application/json" };
    }
    const res = await doFetch(base + "/api/v1" + path, init);
    if (res.status === 204) return undefined as T;
    const text = await res.text();
    let data: unknown = null;
    try {
      data = text ? JSON.parse(text) : null;
    } catch {
      data = null;
    }
    if (!res.ok) throw new FlagfallError(res.status, data as ApiErrorBody | null);
    return data as T;
  }

  const get = <T>(path: string) => call<T>("GET", path);
  const post = <T>(path: string, body?: unknown) => call<T>("POST", path, body ?? {});
  const qs = (q: Record<string, string | number | boolean | undefined> = {}) => {
    const p = new URLSearchParams();
    for (const [k, v] of Object.entries(q)) if (v !== undefined && v !== "") p.set(k, String(v));
    const s = p.toString();
    return s ? "?" + s : "";
  };
  const enc = encodeURIComponent;

  const live = createLive(base + "/api/v1/live", options.EventSource);

  return {
    event: { get: () => get<Event>("/event") },

    auth: {
      me: () => get<Me | null>("/me"),
      signIn: (input) => post<Me>("/auth/sign-in", input),
      signUp: (input) => post<Me>("/auth/sign-up", input),
      signOut: () => post<void>("/auth/sign-out"),
      requestPasswordReset: (email) => post<void>("/auth/password-reset", { email }),
      resetPassword: (input) => post<void>("/auth/password-reset/confirm", input),
      updateAccount: (input) => call<Me>("PATCH", "/me", input),
      changePassword: (input) => post<void>("/me/password", input),
    },

    teams: {
      list: (q) => get<Paged<TeamSummary>>("/teams" + qs(q)),
      get: (id) => get<Team | MyTeam>("/teams/" + enc(id)),
      create: (input) => post<MyTeam>("/teams", input),
      join: (inviteCode) => post<MyTeam>("/teams/join", { inviteCode }),
      leave: () => post<void>("/teams/leave"),
      update: (input) => call<MyTeam>("PATCH", "/teams/mine", input),
      regenerateInviteCode: () => post<MyTeam>("/teams/mine/invite-code"),
      removeMember: (userId) => call<MyTeam>("DELETE", "/teams/mine/members/" + enc(userId)),
      transferCaptain: (userId) => post<MyTeam>("/teams/mine/captain", { userId }),
    },

    challenges: {
      list: () => get<ChallengeSummary[]>("/challenges"),
      get: (slug) => get<Challenge>("/challenges/" + enc(slug)),
      submitFlag: (slug, flag) => post<FlagResult>("/challenges/" + enc(slug) + "/submit", { flag }),
      unlockHint: (slug, hintId) => post<{ id: ID; content: string }>("/challenges/" + enc(slug) + "/hints/" + enc(hintId) + "/unlock"),
    },

    scoreboard: {
      get: (q) => get<Scoreboard>("/scoreboard" + qs(q)),
      series: (q) => get<ScoreSeries[]>("/scoreboard/series" + qs(q)),
    },

    announcements: { list: () => get<Announcement[]>("/announcements") },

    pages: {
      get: (slug) => get<Page>("/pages/" + enc(slug)),
      list: () => get<Pick<Page, "slug" | "title">[]>("/pages"),
    },

    live,

    loadTheme: async () => createThemeContext(await get<ThemeInfo>("/theme")),
  };
}

/** True if err is a FlagfallError with one of the given codes. */
export function isError(err: unknown, ...codes: ErrorCode[]): err is FlagfallError {
  return err instanceof FlagfallError && (codes.length === 0 || codes.includes(err.code));
}
