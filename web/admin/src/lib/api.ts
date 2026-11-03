/** Client for /admin/api/v1 (and the few player endpoints the panel needs). */

export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
    public fields: Record<string, string> = {},
    public problems: { path: string; message: string }[] = [],
  ) {
    super(message);
  }
}

const ADMIN = "/admin/api/v1";

async function request<T>(method: string, url: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = {};
  const init: RequestInit = { method, credentials: "same-origin", headers };
  if (body instanceof FormData) {
    init.body = body;
  } else if (body !== undefined) {
    init.body = JSON.stringify(body);
    headers["Content-Type"] = "application/json";
  }
  const res = await fetch(url, init);
  if (res.status === 204) return undefined as T;
  const text = await res.text();
  let data: unknown = null;
  try {
    data = text ? JSON.parse(text) : null;
  } catch {
    throw new ApiError(res.status, "bad_response", `The server answered ${res.status}.`);
  }
  if (!res.ok) {
    type Body = { error?: { code?: string; message?: string; fields?: Record<string, string>; problems?: { path: string; message: string }[] } };
    const e = (data as Body | null)?.error;
    throw new ApiError(res.status, e?.code ?? "error", e?.message ?? `Request failed (${res.status}).`, e?.fields ?? {}, e?.problems ?? []);
  }
  return data as T;
}

export const api = {
  get: <T>(path: string) => request<T>("GET", ADMIN + path),
  post: <T>(path: string, body?: unknown) => request<T>("POST", ADMIN + path, body ?? {}),
  patch: <T>(path: string, body: unknown) => request<T>("PATCH", ADMIN + path, body),
  put: <T>(path: string, body: unknown) => request<T>("PUT", ADMIN + path, body),
  del: <T = void>(path: string) => request<T>("DELETE", ADMIN + path),
  upload: <T>(path: string, field: string, file: File) => {
    const fd = new FormData();
    fd.append(field, file);
    return request<T>("POST", ADMIN + path, fd);
  },
  signIn: (email: string, password: string) => request<unknown>("POST", "/api/v1/auth/sign-in", { email, password }),
  signOut: () => request<void>("POST", "/api/v1/auth/sign-out", {}),
};

/** Builds a query string, skipping empty values. */
export function qs(params: Record<string, string | number | boolean | null | undefined>): string {
  const u = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) if (v !== "" && v !== null && v !== undefined) u.set(k, String(v));
  const s = u.toString();
  return s ? "?" + s : "";
}

export function message(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

export function fieldErrors(err: unknown): Record<string, string> {
  return err instanceof ApiError ? err.fields : {};
}
