export type Role = "player" | "support" | "author" | "admin" | "owner";
export const roles: Role[] = ["player", "support", "author", "admin", "owner"];
export const roleRank: Record<Role, number> = { player: 0, support: 1, author: 2, admin: 3, owner: 4 };

export interface Me { id: string; name: string; email: string; role: Role }

export interface Event {
  name: string; state: string;
  startsAt: string | null; endsAt: string | null; freezeAt: string | null;
  timeZone: string; maxTeamSize: number; scoringMode: string;
  registrationOpen: boolean; teamsLocked: boolean; publicChallenges: boolean;
  flagRateLimit: number; updatedAt: string;
}

export interface Bracket { id: string; name: string; description: string; requiresVerification: boolean; position: number; teams: number }

export interface ChallengeSummary {
  id: string; slug: string; name: string; category: string; difficulty: string | null;
  state: string; visibleFrom: string | null; value: number; currentValue: number;
  solves: number; attempts: number; position: number;
}
export interface Flag { id: string; kind: string; pattern: string | null }
export interface Hint { id: string; cost: number; content: string; unlocks: number }
export interface FileInfo { id: string; name: string; size: number; sha256: string; url: string }
export interface Challenge extends ChallengeSummary {
  author: string; description: string; connection: string | null;
  minValue: number | null; decay: number | null; tags: string[];
  themeFields: Record<string, unknown>; themeFieldWarnings: string[]; otherThemeFields: string[]; flags: Flag[]; hints: Hint[]; files: FileInfo[];
}

export interface User {
  id: string; name: string; email: string; role: Role; country: string | null;
  teamId: string | null; teamName: string | null; verified: boolean; banned: boolean;
  createdAt: string; lastSeenAt: string | null;
}
export interface Team {
  id: string; name: string; country: string | null; bracketId: string; inviteCode: string;
  captainId: string | null; hidden: boolean; banned: boolean; prizeEligible: boolean; recruiting: boolean;
  memberCount: number; score: number; rank: number | null; createdAt: string;
}
export interface TeamDetail { team: Team; members: User[]; solves: { challengeId: string; slug: string; name: string; solvedAt: string }[] }
export interface Paged<T> { items: T[]; total: number; page: number; pageSize: number }

export interface Announcement { id: string; title: string; body: string; pinned: boolean; postAt: string; posted: boolean; bodyHtml: string }
export interface Page { slug: string; title: string; body: string; visible: boolean; position: number; updatedAt: string }

export interface BoardEntry {
  rank: number | null; teamId: string; teamName: string; country: string | null; bracketId: string;
  hidden: boolean; banned: boolean; score: number; solveCount: number; firstBloods: number; lastSolveAt: string | null;
}
export interface Board { frozen: boolean; freezeAt: string | null; updatedAt: string; entries: BoardEntry[] }
export interface Adjustment { id: string; teamId: string; teamName: string; points: number; reason: string; actorName: string | null; createdAt: string }

export interface Submission {
  id: string; teamId: string; teamName: string; userId: string | null; userName: string | null;
  challengeId: string; challengeSlug: string; challengeName: string;
  correct: boolean; submitted: string; ip: string | null; submittedAt: string;
}

export interface Dashboard {
  state: string; playersOnline: number; teams: number; players: number; submissions: number;
  submissionsPerMinute: number; solves: number; warnings: string[];
  challenges: { id: string; slug: string; name: string; category: string; state: string; solves: number; attempts: number; firstBloodTeam: string | null; firstBloodAt: string | null }[];
}

export interface AuditEntry { id: string; actorId: string | null; actorName: string | null; action: string; target: string | null; details: string | null; ip: string | null; at: string }

export type ChallengeFieldType = "text" | "number" | "boolean" | "select" | "location";
export interface ChallengeField {
  key: string; type: ChallengeFieldType; label: string; help?: string; required?: boolean;
  options?: { value: string; label: string }[];
}
export interface ChallengeFields { theme: { id: string; name: string }; fields: ChallengeField[] }
