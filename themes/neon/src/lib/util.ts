const nf = new Intl.NumberFormat("en-US");

export const fmt = (n: number) => nf.format(n);

/** "kernel_panic" -> "KP", "0xdeadbeef" -> "0X", "hax44" -> "H". */
export function initials(name: string): string {
  const parts = name.split(/[_\s.-]+/).filter(Boolean);
  if (parts.length >= 2) return (parts[0]![0]! + parts[1]![0]!).toUpperCase();
  return name.slice(0, name.length > 6 ? 2 : 1).toUpperCase();
}

export const difficultyBars: Record<string, number> = { easy: 1, medium: 2, hard: 3, insane: 3 };

export const cap = (s: string) => (s ? s[0]!.toUpperCase() + s.slice(1) : s);

export function formatDate(iso: string | null | undefined, opts: Intl.DateTimeFormatOptions = {}): string {
  if (!iso) return "";
  return new Intl.DateTimeFormat("en-GB", {
    weekday: "short", day: "numeric", month: "short", hour: "2-digit", minute: "2-digit", timeZone: "UTC", ...opts,
  }).format(new Date(iso)) + (opts.hour === undefined && opts.timeZone === undefined ? " UTC" : "");
}

export function errorMessage(err: unknown, t: (k: string) => string): string {
  const code = (err as { code?: string })?.code;
  const msg = (err as { message?: string })?.message;
  return msg || (code ? t("error." + code) : t("error.internal"));
}
