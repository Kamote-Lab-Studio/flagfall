import type { LiveConnection, LiveStatus } from "./index.js";

type Handler = (data: any) => void;

// live.refetch() waits REFETCH_MIN plus up to REFETCH_SPREAD milliseconds.
const REFETCH_MIN = 1000;
const REFETCH_SPREAD = 3000;

const EVENT_TYPES = ["scoreboard", "solve", "announcement", "challenge-updated", "event-state", "theme-published", "resync"] as const;

/**
 * Opens one EventSource lazily, on the first subscription. The browser
 * reconnects on its own and sends Last-Event-ID, so missed events are
 * replayed by the server.
 */
export function createLive(url: string, ES: typeof EventSource | undefined): LiveConnection {
  const handlers = new Map<string, Set<Handler>>();
  const statusHandlers = new Set<(s: LiveStatus) => void>();
  let source: EventSource | null = null;
  let status: LiveStatus = "closed";
  let closed = false;
  // Set as soon as an open is scheduled, so several on() calls in a row
  // share one connection (browsers allow only ~6 per host over HTTP/1.1).
  let pending = false;

  const setStatus = (s: LiveStatus) => {
    if (s === status) return;
    status = s;
    statusHandlers.forEach((h) => h(s));
  };

  const dispatch = (type: string, raw: string) => {
    let data: unknown = {};
    try {
      data = raw ? JSON.parse(raw) : {};
    } catch {
      return;
    }
    const hs = handlers.get(type);
    if (hs && hs.size > 0) {
      hs.forEach((h) => h(data));
    } else if (type === "theme-published" && typeof location !== "undefined") {
      location.reload();
    }
  };

  const open = () => {
    if (source || pending || closed) return;
    const Ctor = ES ?? (typeof EventSource !== "undefined" ? EventSource : undefined);
    if (!Ctor) return;
    // Jitter, so a thousand tabs don't all reconnect in the same instant.
    const delay = status === "reconnecting" ? Math.random() * 2000 : 0;
    pending = true;
    setStatus("connecting");
    setTimeout(() => {
      pending = false;
      if (closed) return;
      source = new Ctor(url, { withCredentials: true });
      source.onopen = () => setStatus("open");
      source.onerror = () => {
        if (source && source.readyState === 2 /* CLOSED */) {
          source = null;
          setStatus("reconnecting");
          setTimeout(open, 3000);
        } else {
          setStatus("reconnecting");
        }
      };
      for (const t of EVENT_TYPES) {
        source.addEventListener(t, (ev) => dispatch(t, (ev as MessageEvent).data));
      }
    }, delay);
  };

  const live: LiveConnection = {
    refetch(types, fn) {
      let timer: ReturnType<typeof setTimeout> | null = null;
      const fire = () => {
        timer = null;
        fn();
      };
      const schedule = () => {
        if (timer === null) timer = setTimeout(fire, REFETCH_MIN + Math.random() * REFETCH_SPREAD);
      };
      const offs = types.map((t) => live.on(t, schedule));
      return () => {
        offs.forEach((off) => off());
        if (timer !== null) clearTimeout(timer);
      };
    },
    on(type, handler) {
      let set = handlers.get(type);
      if (!set) handlers.set(type, (set = new Set()));
      set.add(handler as Handler);
      open();
      return () => set!.delete(handler as Handler);
    },
    get status() {
      return status;
    },
    onStatus(handler) {
      statusHandlers.add(handler);
      return () => statusHandlers.delete(handler);
    },
    close() {
      closed = true;
      source?.close();
      source = null;
      setStatus("closed");
    },
  };
  return live;
}
