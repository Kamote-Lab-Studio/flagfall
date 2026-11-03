type T = (key: string, vars?: Record<string, string | number>) => string;

/** "12m ago", "8h 25m ago", using the theme's translations. */
export function relativeTime(iso: string, t: T, now: Date = new Date()): string {
  const secs = Math.max(0, Math.floor((now.getTime() - new Date(iso).getTime()) / 1000));
  if (secs < 10) return t("time.justNow");
  if (secs < 60) return t("time.secondsAgo", { n: secs });
  const mins = Math.floor(secs / 60);
  if (mins < 60) return t("time.minutesAgo", { n: mins });
  const hours = Math.floor(mins / 60);
  if (hours < 48) return t("time.hoursAgo", { h: hours, m: String(mins % 60).padStart(2, "0") });
  return t("time.daysAgo", { n: Math.floor(hours / 24) });
}

/** "14:22:07" for a countdown; hours can exceed 24. */
export function formatDuration(ms: number): string {
  const s = Math.max(0, Math.floor(ms / 1000));
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const sec = s % 60;
  return [h, m, sec].map((n) => String(n).padStart(2, "0")).join(":");
}
