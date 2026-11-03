import { describe, expect, it, vi } from "vitest";
import { createClient, FlagfallError, formatDuration, isError, relativeTime } from "./index.js";
import { createThemeContext } from "./theme.js";

function fakeFetch(routes: Record<string, { status?: number; body?: unknown }>) {
  const calls: { url: string; init: RequestInit }[] = [];
  const fn = vi.fn(async (url: string, init: RequestInit) => {
    calls.push({ url, init });
    const key = `${init.method} ${url}`;
    const r = routes[key];
    if (!r) return new Response(JSON.stringify({ error: { code: "not_found", message: "Not found." } }), { status: 404 });
    const status = r.status ?? 200;
    return new Response(status === 204 ? null : JSON.stringify(r.body ?? null), { status });
  });
  return { fn: fn as unknown as typeof fetch, calls };
}

describe("client", () => {
  it("sends JSON with the right method and path", async () => {
    const { fn, calls } = fakeFetch({
      "POST /api/v1/challenges/cookie-jar/submit": { body: { status: "correct", points: 100, firstBlood: true } },
    });
    const ff = createClient({ fetch: fn });
    const r = await ff.challenges.submitFlag("cookie-jar", "flag{x}");
    expect(r).toEqual({ status: "correct", points: 100, firstBlood: true });
    expect(calls[0]!.init.body).toBe(JSON.stringify({ flag: "flag{x}" }));
    expect((calls[0]!.init.headers as Record<string, string>)["Content-Type"]).toBe("application/json");
  });

  it("returns null from me() when signed out", async () => {
    const { fn } = fakeFetch({ "GET /api/v1/me": { body: null } });
    expect(await createClient({ fetch: fn }).auth.me()).toBeNull();
  });

  it("throws FlagfallError with code and fields", async () => {
    const { fn } = fakeFetch({
      "POST /api/v1/auth/sign-up": { status: 400, body: { error: { code: "validation", message: "Check.", fields: { email: "Bad." } } } },
    });
    const err = await createClient({ fetch: fn }).auth.signUp({ name: "a", email: "b", password: "c" }).catch((e) => e);
    expect(err).toBeInstanceOf(FlagfallError);
    expect(err.code).toBe("validation");
    expect(err.fields.email).toBe("Bad.");
    expect(isError(err, "validation")).toBe(true);
    expect(isError(err, "forbidden")).toBe(false);
  });

  it("handles 204 and builds query strings", async () => {
    const { fn, calls } = fakeFetch({
      "POST /api/v1/auth/sign-out": { status: 204 },
      "GET /api/v1/teams?search=kernel&page=2": { body: { items: [], total: 0, page: 2, pageSize: 50 } },
    });
    const ff = createClient({ fetch: fn });
    await expect(ff.auth.signOut()).resolves.toBeUndefined();
    await ff.teams.list({ search: "kernel", page: 2, bracketId: undefined });
    expect(calls[1]!.url).toBe("/api/v1/teams?search=kernel&page=2");
  });
});

describe("theme", () => {
  it("translates with organizer overrides, theme messages and defaults", async () => {
    const { fn } = fakeFetch({
      "GET /api/v1/theme": { body: { id: "amber-phosphor", version: 1, settings: { accent: "#FFB000" }, preview: false, locale: "en", messages: { "board.title": "Missions" } } },
    });
    const theme = await createClient({ fetch: fn }).loadTheme();
    theme.addMessages("en", { "board.title": "Challenges", "board.tagline": "Find the flag." });
    expect(theme.t("board.title")).toBe("Missions");
    expect(theme.t("board.tagline")).toBe("Find the flag.");
    expect(theme.t("flag.correct", { points: 50 })).toBe("Correct! +50 points.");
    expect(theme.t("no.such.key")).toBe("no.such.key");
    expect(theme.settings.accent).toBe("#FFB000");
  });
});

describe("format", () => {
  const t = (k: string, v?: Record<string, string | number>) => k + JSON.stringify(v ?? {});
  it("formats relative times and countdowns", () => {
    const now = new Date("2026-11-03T12:00:00Z");
    expect(relativeTime("2026-11-03T11:48:00Z", t, now)).toBe('time.minutesAgo{"n":12}');
    expect(relativeTime("2026-11-03T03:35:00Z", t, now)).toBe('time.hoursAgo{"h":8,"m":"25"}');
    expect(formatDuration((14 * 3600 + 22 * 60 + 7) * 1000)).toBe("14:22:07");
    expect(formatDuration(-5)).toBe("00:00:00");
  });
});

describe("live", () => {
  it("opens one connection no matter how many handlers subscribe", async () => {
    vi.useFakeTimers();
    const opened: string[] = [];
    class FakeES {
      readyState = 0;
      onopen: (() => void) | null = null;
      onerror: (() => void) | null = null;
      constructor(url: string) {
        opened.push(url);
      }
      addEventListener() {}
      close() {}
    }
    const ff = createClient({ EventSource: FakeES as unknown as typeof EventSource });
    for (const type of ["scoreboard", "solve", "announcement", "challenge-updated", "event-state", "resync"] as const) {
      ff.live.on(type, () => {});
    }
    await vi.runAllTimersAsync();
    ff.live.on("scoreboard", () => {});
    await vi.runAllTimersAsync();
    expect(opened).toEqual(["/api/v1/live"]);
    ff.live.close();
    vi.useRealTimers();
  });
});

describe("preview", () => {
  const info = { id: "neon", version: 2, settings: {}, preview: true, locale: "en", messages: {} };

  it("reloads a preview tab when the admin saves this theme's draft or ends preview", () => {
    const channel: { onmessage: ((ev: { data: unknown }) => void) | null } = { onmessage: null };
    const reload = vi.fn();
    createThemeContext(info, { channel, reload });
    channel.onmessage!({ data: { type: "draft-saved", themeId: "other" } });
    channel.onmessage!({ data: "junk" });
    expect(reload).not.toHaveBeenCalled();
    channel.onmessage!({ data: { type: "draft-saved", themeId: "neon" } });
    channel.onmessage!({ data: { type: "preview-ended", themeId: "neon" } });
    expect(reload).toHaveBeenCalledTimes(2);
  });

  it("doesn't listen outside preview", () => {
    const channel: { onmessage: ((ev: { data: unknown }) => void) | null } = { onmessage: null };
    createThemeContext({ ...info, preview: false }, { channel, reload: vi.fn() });
    expect(channel.onmessage).toBeNull();
  });
});
