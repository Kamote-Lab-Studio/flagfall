/** A small history-API router. Same-origin <a> clicks are handled automatically. */

export const route = $state({
  path: location.pathname,
  query: new URLSearchParams(location.search),
});

function update() {
  route.path = location.pathname;
  route.query = new URLSearchParams(location.search);
}

export function navigate(to: string, opts: { replace?: boolean } = {}) {
  if (to === location.pathname + location.search) return;
  if (opts.replace) history.replaceState(null, "", to);
  else history.pushState(null, "", to);
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

const passthrough = ["/api/", "/files/", "/theme/", "/admin"];

export function installLinkHandler() {
  window.addEventListener("popstate", update);
  document.addEventListener("click", (ev) => {
    if (ev.defaultPrevented || ev.button !== 0 || ev.metaKey || ev.ctrlKey || ev.shiftKey || ev.altKey) return;
    const a = (ev.target as Element | null)?.closest?.("a");
    if (!a || a.target || a.hasAttribute("download")) return;
    const url = new URL(a.href, location.href);
    if (url.origin !== location.origin || passthrough.some((p) => url.pathname.startsWith(p))) return;
    if (url.pathname === location.pathname && url.hash) return; // in-page anchor
    ev.preventDefault();
    navigate(url.pathname + url.search);
  });
}
