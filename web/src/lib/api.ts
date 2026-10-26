import type { Session } from "./types";
let csrf = "";
export class APIError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message);
  }
}
export async function api<T>(
  path: string,
  options: RequestInit = {},
): Promise<T> {
  const response = await fetch(`/api/v1${path}`, {
    ...options,
    credentials: "same-origin",
    cache: "no-store",
    headers: {
      "Content-Type": "application/json",
      ...(csrf ? { "X-CSRF-Token": csrf } : {}),
      ...options.headers,
    },
  });
  if (!response.ok) {
    if (response.status === 401 && path !== "/auth/login")
      window.dispatchEvent(new Event("session-expired"));
    const body = await response.json().catch(() => ({}));
    throw new APIError(
      response.status,
      body.error || `请求失败 (${response.status})`,
    );
  }
  if (response.status === 204) return undefined as T;
  return response.json();
}
export async function fetchSession(): Promise<Session | null> {
  try {
    const result = await api<Session>("/auth/session");
    csrf = result.csrf_token;
    return result;
  } catch (e) {
    if (e instanceof APIError && e.status === 401) {
      csrf = "";
      return null;
    }
    throw e;
  }
}
export async function login(username: string, password: string) {
  const result = await api<Session>("/auth/login", {
    method: "POST",
    body: JSON.stringify({ username, password }),
  });
  csrf = result.csrf_token;
  return result;
}
export async function logout() {
  await api("/auth/logout", { method: "POST" });
  csrf = "";
}
