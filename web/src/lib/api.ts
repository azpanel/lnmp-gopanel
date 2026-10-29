import type { LoginChallenge, Session } from "./types";
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
  observeResponse?: (response: Response) => void,
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
  observeResponse?.(response);
  if (!response.ok) {
    if (
      response.status === 401 &&
      !path.startsWith("/auth/login") &&
      path !== "/auth/session"
    )
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
export async function fetchSession(
  notifyExpired = true,
): Promise<Session | null> {
  try {
    const result = await api<Session>("/auth/session");
    csrf = result.csrf_token;
    return result;
  } catch (e) {
    if (e instanceof APIError && e.status === 401) {
      if (notifyExpired) {
        csrf = "";
        window.dispatchEvent(new Event("session-expired"));
      }
      return null;
    }
    throw e;
  }
}
export async function refreshStreamSession() {
  const session = await fetchSession(false);
  if (session) return session;
  // Allow an in-flight credential change to deliver its rotated cookie before
  // deciding that a stream using the previous cookie means this tab logged out.
  await new Promise((resolve) => setTimeout(resolve, 200));
  return fetchSession();
}
export async function login(username: string, password: string) {
  const result = await api<Session | LoginChallenge>("/auth/login", {
    method: "POST",
    body: JSON.stringify({ username, password }),
  });
  if (!("totp_required" in result)) acceptSession(result);
  return result;
}
export function acceptSession(session: Session) {
  csrf = session.csrf_token;
  return session;
}
export async function loginTOTP(
  challenge_token: string,
  code: string,
  recovery: boolean,
) {
  const session = await api<Session>("/auth/login/totp", {
    method: "POST",
    body: JSON.stringify({
      challenge_token,
      ...(recovery ? { recovery_code: code } : { code }),
    }),
  });
  return acceptSession(session);
}
export async function logout() {
  await api("/auth/logout", { method: "POST" });
  csrf = "";
}
