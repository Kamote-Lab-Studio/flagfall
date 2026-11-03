/**
 * @flagfall/theme-sdk: public API.
 *
 * PHASE 0 STATUS: this file is the design, written as type signatures only.
 * It is type-checked in CI so the design stays consistent; the implementation
 * comes in Phase 1. Review comments go on the pull request that changes it.
 *
 * Rules the SDK enforces for themes:
 *  - It only talks to the player API (/api/v1). It has no admin functions,
 *    and the server rejects player sessions on /admin routes anyway.
 *  - Errors are thrown as FlagfallError with a stable `code`.
 *  - Everything a theme shows that can change during an event (scoreboard,
 *    solves, announcements, event state, theme) is available live.
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

export type * from "./types.js";

// ---------------------------------------------------------------- Client

export interface ClientOptions {
  /** Defaults to the site the theme is served from. */
  baseUrl?: string;
  /** Override fetch, for tests. */
  fetch?: typeof fetch;
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
    list(query?: { bracketId?: ID; search?: string; recruiting?: boolean; page?: number }): Promise<Paged<TeamSummary>>;
    get(id: ID): Promise<Team>;
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
    series(query?: { bracketId?: ID; top?: number }): Promise<ScoreSeries[]>;
  };

  announcements: {
    list(): Promise<Announcement[]>;
  };

  pages: {
    get(slug: string): Promise<Page>;
    list(): Promise<Pick<Page, "slug" | "title">[]>;
  };

  live: LiveConnection;
  theme: ThemeContext;
}

export declare function createClient(options?: ClientOptions): Flagfall;

// ---------------------------------------------------------------- Live updates

export type LiveStatus = "connecting" | "open" | "reconnecting" | "closed";

export interface LiveConnection {
  /**
   * Listen for one type of live event. Returns a function that removes the
   * listener. The connection opens on the first subscription and reconnects
   * with backoff on its own.
   *
   * "theme-published" is handled by the SDK by default (it reloads the page);
   * subscribing replaces that behaviour.
   */
  on<T extends LiveEventType>(type: T, handler: (data: LiveEventData<T>) => void): () => void;
  readonly status: LiveStatus;
  onStatus(handler: (status: LiveStatus) => void): () => void;
  close(): void;
}

// ---------------------------------------------------------------- Theme context

/** Setting values, keyed by the names in the theme's theme.json settings schema. */
export type SettingValues = Record<string, string | number | boolean | null>;

export interface ThemeContext {
  /** The theme's id from theme.json. */
  readonly id: string;
  /** Published settings, or the draft while an admin is previewing. */
  readonly settings: SettingValues;
  /** True in admin preview mode; themes should keep the preview banner visible. */
  readonly preview: boolean;
  /** Current locale, e.g. "en" or "fil". */
  readonly locale: string;
  /**
   * Translate a message key. Looks up the organizer's wording overrides first,
   * then the theme's message files, then the SDK defaults.
   */
  t(key: string, vars?: Record<string, string | number>): string;
  /** Fires when settings change during preview. */
  onSettingsChange(handler: (settings: SettingValues) => void): () => void;
}

// ---------------------------------------------------------------- Errors

export declare class FlagfallError extends Error {
  readonly code: ErrorCode;
  readonly status: number;
  readonly fields: Record<string, string>;
  constructor(status: number, body: ApiErrorBody);
}
