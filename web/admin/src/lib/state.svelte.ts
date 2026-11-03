import type { Me, Role } from "./types";
import { roleRank } from "./types";

export const session = $state<{ me: Me | null }>({ me: null });

/** Whether the signed-in staff member has at least this role. */
export function can(min: Role): boolean {
  return !!session.me && roleRank[session.me.role] >= roleRank[min];
}

/** A short-lived message at the bottom of the screen. */
export const toast = $state<{ text: string; kind: "ok" | "error"; n: number }>({ text: "", kind: "ok", n: 0 });

export function notify(text: string, kind: "ok" | "error" = "ok") {
  toast.text = text;
  toast.kind = kind;
  const n = ++toast.n;
  setTimeout(() => {
    if (toast.n === n) toast.text = "";
  }, 4000);
}
