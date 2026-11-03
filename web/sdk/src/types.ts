/**
 * Data types returned by the Flagfall player API.
 *
 * These match the JSON in docs/dev/api.md one to one. Timestamps are
 * ISO 8601 strings in UTC; IDs are strings so they can change format later
 * without breaking themes.
 */

export type ID = string;
export type ISODate = string;

// ---------------------------------------------------------------- Event

/** Where the event is in its life cycle. Themes show a different screen for each. */
export type EventState = "not_started" | "running" | "frozen" | "over";

export type ScoringMode = "static" | "dynamic";

export interface Bracket {
  id: ID;
  name: string;
  description: string;
}

export interface Event {
  name: string;
  state: EventState;
  startsAt: ISODate;
  endsAt: ISODate;
  /** Null when the scoreboard never freezes. */
  freezeAt: ISODate | null;
  /** Server time when the response was made, so themes can correct clock drift in countdowns. */
  serverTime: ISODate;
  timeZone: string;
  maxTeamSize: number;
  scoringMode: ScoringMode;
  registrationOpen: boolean;
  brackets: Bracket[];
  /** Category names in display order, for filters. */
  categories: string[];
  /** A public login to show on the sign-in page. Null except on demo installs. */
  demoAccount: { email: string; password: string } | null;
}

// ---------------------------------------------------------------- Accounts and teams

export interface UserSummary {
  id: ID;
  name: string;
  country: string | null;
}

/** The signed-in player. */
export interface Me extends UserSummary {
  email: string;
  verified: boolean;
  /** Null when the player is not on a team yet. */
  team: MyTeam | null;
}

export interface TeamSummary {
  id: ID;
  name: string;
  /** ISO 3166-1 alpha-2 code, or null. */
  country: string | null;
  bracketId: ID;
  memberCount: number;
  recruiting: boolean;
  /** Null when the team is hidden from the scoreboard or has no points yet. */
  rank: number | null;
  score: number;
}

export interface TeamMember extends UserSummary {
  role: "captain" | "player";
  solveCount: number;
  score: number;
  lastSolveAt: ISODate | null;
}

export interface Team extends TeamSummary {
  createdAt: ISODate;
  members: TeamMember[];
  solves: TeamSolve[];
  firstBloodCount: number;
  /** Points per category, for the team profile chart. */
  scoreByCategory: { category: string; score: number }[];
}

/** Extra fields only the team's own members see. */
export interface MyTeam extends Team {
  inviteCode: string;
  isCaptain: boolean;
  unlockedHintIds: ID[];
}

export interface TeamSolve {
  challengeSlug: string;
  challengeName: string;
  category: string;
  userName: string;
  points: number;
  solvedAt: ISODate;
  firstBlood: boolean;
}

// ---------------------------------------------------------------- Challenges

export type Difficulty = "easy" | "medium" | "hard" | "insane";

export interface ChallengeSummary {
  slug: string;
  name: string;
  category: string;
  difficulty: Difficulty;
  /** Current value (changes over time under dynamic scoring). */
  value: number;
  solveCount: number;
  /** True when the signed-in player's team has solved it. */
  solved: boolean;
  /** Extra fields declared by the active theme's manifest, e.g. a map location. */
  themeFields: Record<string, unknown>;
}

export interface Hint {
  id: ID;
  cost: number;
  /** Null until the team unlocks it. Free hints (cost 0) are always included. */
  content: string | null;
}

export interface ChallengeFile {
  name: string;
  size: number;
  sha256: string;
  /** Download URL under /files/. */
  url: string;
}

export interface Challenge extends ChallengeSummary {
  /** Sanitized HTML rendered from the author's Markdown. */
  descriptionHtml: string;
  author: string;
  /** Connection string or URL with {{event_domain}} already filled in. */
  connection: string | null;
  files: ChallengeFile[];
  hints: Hint[];
  firstBlood: { teamId: ID; teamName: string; solvedAt: ISODate } | null;
}

export type FlagResult =
  | { status: "correct"; points: number; firstBlood: boolean }
  | { status: "incorrect" }
  | { status: "already_solved" }
  | { status: "rate_limited"; retryAfterSeconds: number }
  | { status: "event_not_running"; state: EventState };

// ---------------------------------------------------------------- Scoreboard

export interface ScoreboardEntry {
  rank: number;
  team: Pick<TeamSummary, "id" | "name" | "country" | "bracketId">;
  score: number;
  solveCount: number;
  lastSolveAt: ISODate | null;
}

export interface Scoreboard {
  bracketId: ID | null;
  /** True while the scoreboard is frozen; entries show the state at freezeAt. */
  frozen: boolean;
  updatedAt: ISODate;
  entries: ScoreboardEntry[];
}

/** One line on the score-over-time chart. */
export interface ScoreSeries {
  teamId: ID;
  teamName: string;
  points: { at: ISODate; score: number }[];
}

// ---------------------------------------------------------------- Content

export interface Announcement {
  id: ID;
  title: string;
  bodyHtml: string;
  pinned: boolean;
  postedAt: ISODate;
}

export interface Page {
  slug: string;
  title: string;
  bodyHtml: string;
}

// ---------------------------------------------------------------- Live updates

/** Messages pushed over Server-Sent Events. See docs/dev/events.md. */
export type LiveEvent =
  | { type: "scoreboard"; data: { bracketId: ID | null; updatedAt: ISODate } }
  | { type: "solve"; data: { teamId: ID; teamName: string; challengeSlug: string; points: number; firstBlood: boolean; at: ISODate } }
  | { type: "announcement"; data: Announcement }
  | { type: "challenge-updated"; data: { slug: string } }
  | { type: "event-state"; data: { state: EventState } }
  | { type: "theme-published"; data: { themeId: string; version: number } };

export type LiveEventType = LiveEvent["type"];
export type LiveEventData<T extends LiveEventType> = Extract<LiveEvent, { type: T }>["data"];

// ---------------------------------------------------------------- Errors

/** Machine-readable error codes in API error responses. */
export type ErrorCode =
  | "unauthenticated"
  | "forbidden"
  | "not_found"
  | "validation"
  | "rate_limited"
  | "conflict"
  | "event_not_running"
  | "registration_closed"
  | "team_full"
  | "internal";

export interface ApiErrorBody {
  error: {
    code: ErrorCode;
    message: string;
    /** Per-field messages for "validation" errors. */
    fields?: Record<string, string>;
  };
}
