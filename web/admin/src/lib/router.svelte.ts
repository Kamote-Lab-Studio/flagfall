/** History-API router for paths under /admin. Same-origin links to /admin/... are handled. */

const BASE = "/admin";

function current() {
  const p = location.pathname.startsWith(BASE) ? location.pathname.slice(BASE.length) : location.pathname;
  return p === "" ? "/" : p;
}

export const route = $state({ path: current(), query: new URLSearchParams(location.search) });

function update() {
  route.path = current();
  route.query = new URLSearchParams(location.search);
}

/** The full URL for a panel path, e.g. href("/teams/12") → "/admin/teams/12". */
export function href(to: string) {
  return BASE + (to === "/" ? "" : to);
}

/** Go to a panel path, e.g. navigate("/teams/12"). */
export function navigate(to: string, opts: { replace?: boolean } = {}) {
  const full = href(to);
  if (full === location.pathname + location.search) return;
  if (opts.replace) history.replaceState(null, "", full);
  else history.pushState(null, "", full);
  update();
  window.scrollTo(0, 0);
}

/** Match "/teams/:id" against a path; returns params or null. */
export function match(pattern: string, path: string): Record<string, string> | null {
  const p = pattern.split("/").filter(Boolean);
  const s = path.split("/").filter(Boolean);
  if (p.length !== s.length) return null;
  const params: Record<string, string> = {};
  for (let i = 0; i < p.length; i++) {
    const part = p[i]!;
    if (part.startsWith(":")) params[part.slice(1)] = decodeURIComponent(s[i]!);
    else if (part !== s[i]) return null;
  }
  return params;
}

export function installLinkHandler() {
  window.addEventListener("popstate", update);
  document.addEventListener("click", (ev) => {
    if (ev.defaultPrevented || ev.button !== 0 || ev.metaKey || ev.ctrlKey || ev.shiftKey || ev.altKey) return;
    const a = (ev.target as Element | null)?.closest?.("a");
    if (!a || a.target || a.hasAttribute("download")) return;
    const url = new URL(a.href, location.href);
    if (url.origin !== location.origin) return;
    const inPanel = url.pathname === BASE || url.pathname.startsWith(BASE + "/");
    if (!inPanel || url.pathname.startsWith(BASE + "/api/")) return;
    ev.preventDefault();
    navigate((url.pathname.slice(BASE.length) || "/") + url.search);
  });
}
