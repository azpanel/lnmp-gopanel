import type { LoginChallenge, Session } from "./types";
import {
  beginSession,
  captureSession,
  expireSession,
  getCSRF,
  setCSRF,
  trackRequest,
} from "./session-scope";
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
  const scope = captureSession();
  const controller = new AbortController();
  const abort = () => controller.abort();
  options.signal?.addEventListener("abort", abort, { once: true });
  if (options.signal?.aborted) controller.abort();
  const untrack = trackRequest(controller);
  const csrf = getCSRF();
  try {
    const response = await fetch(`/api/v1${path}`, {
      ...options,
      signal: controller.signal,
      credentials: "same-origin",
      cache: "no-store",
      headers: {
        "Content-Type": "application/json",
        ...(csrf ? { "X-CSRF-Token": csrf } : {}),
        ...options.headers,
      },
    });
    scope.assert();
    controller.signal.throwIfAborted();
    observeResponse?.(response);
    if (!response.ok) {
      if (
        response.status === 401 &&
        !path.startsWith("/auth/login") &&
        path !== "/auth/session"
      ) {
        expireSession();
        scope.assert();
      }
      const body = await response.json().catch(() => ({}));
      scope.assert();
      controller.signal.throwIfAborted();
      throw new APIError(
        response.status,
        body.error || `请求失败 (${response.status})`,
      );
    }
    if (response.status === 204) return undefined as T;
    const result = await response.json();
    scope.assert();
    controller.signal.throwIfAborted();
    return result;
  } catch (error) {
    scope.assert();
    throw error;
  } finally {
    untrack();
    options.signal?.removeEventListener("abort", abort);
  }
}
export async function fetchSession(
  notifyExpired = true,
  signal?: AbortSignal,
): Promise<Session | null> {
  const scope = captureSession();
  try {
    const result = await api<Session>("/auth/session", { signal });
    scope.assert();
    signal?.throwIfAborted();
    setCSRF(result.csrf_token);
    return result;
  } catch (e) {
    scope.assert();
    signal?.throwIfAborted();
    if (e instanceof APIError && e.status === 401) {
      if (notifyExpired) {
        expireSession();
      }
      return null;
    }
    throw e;
  }
}
export async function refreshStreamSession(signal?: AbortSignal) {
  const scope = captureSession();
  const session = await fetchSession(false, signal);
  scope.assert();
  if (session) return session;
  // Allow an in-flight credential change to deliver its rotated cookie before
  // deciding that a stream using the previous cookie means this tab logged out.
  await new Promise((resolve) => setTimeout(resolve, 200));
  scope.assert();
  if (signal?.aborted) throw new DOMException("Aborted", "AbortError");
  return fetchSession(true, signal);
}
export async function login(username: string, password: string) {
  const scope = captureSession();
  const result = await api<Session | LoginChallenge>("/auth/login", {
    method: "POST",
    body: JSON.stringify({ username, password }),
  });
  scope.assert();
  if (!("totp_required" in result)) beginSession(result.csrf_token);
  return result;
}
export function acceptSession(session: Session) {
  setCSRF(session.csrf_token);
  return session;
}
export async function loginTOTP(
  challenge_token: string,
  code: string,
  recovery: boolean,
) {
  const scope = captureSession();
  const session = await api<Session>("/auth/login/totp", {
    method: "POST",
    body: JSON.stringify({
      challenge_token,
      ...(recovery ? { recovery_code: code } : { code }),
    }),
  });
  scope.assert();
  beginSession(session.csrf_token);
  return session;
}
export async function logout() {
  const scope = captureSession();
  await api("/auth/logout", { method: "POST" });
  scope.assert();
  expireSession();
}
