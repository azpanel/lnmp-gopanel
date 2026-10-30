import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { MutationObserver, QueryClient } from "@tanstack/react-query";
import { acceptSession, api, fetchSession, login, logout } from "./api";
import {
  beginSession,
  captureSession,
  expireSession,
  getCSRF,
  SessionCancelledError,
} from "./session-scope";
import { sessionMutationOptions } from "./use-session-mutation";

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}
const session = {
  username: "admin",
  csrf_token: "new",
  expires_at: "2030-01-01",
};
beforeEach(() => {
  vi.stubGlobal("window", new EventTarget());
  beginSession("old");
});
afterEach(() => {
  vi.unstubAllGlobals();
});

it("rejects a late session response even if the transport ignores abort", async () => {
  const response = deferred<Response>();
  let signal: AbortSignal | undefined;
  vi.stubGlobal(
    "fetch",
    vi.fn((_url, options) => {
      signal = options.signal;
      return response.promise;
    }),
  );
  const request = fetchSession();
  expireSession();
  expect(signal?.aborted).toBe(true);
  response.resolve(Response.json(session));
  await expect(request).rejects.toBeInstanceOf(SessionCancelledError);
  expect(getCSRF()).toBe("");
});

it("checks the session again after a delayed response body is decoded", async () => {
  const body = deferred<unknown>();
  const decoding = deferred<void>();
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => ({
      ok: true,
      status: 200,
      json: () => {
        decoding.resolve();
        return body.promise;
      },
    })),
  );
  const request = api("/host");
  await decoding.promise;
  expireSession();
  body.resolve({ hostname: "private" });
  await expect(request).rejects.toBeInstanceOf(SessionCancelledError);
});

it("does not let a cancelled session read change CSRF or expire the session", async () => {
  const body = deferred<unknown>();
  const decoding = deferred<void>();
  const controller = new AbortController();
  const expired = vi.fn();
  window.addEventListener("session-expired", expired);
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => ({
      ok: false,
      status: 401,
      json: () => {
        decoding.resolve();
        return body.promise;
      },
    })),
  );
  const request = fetchSession(true, controller.signal);
  await decoding.promise;
  controller.abort();
  body.resolve({ error: "cancelled read" });
  await expect(request).rejects.toMatchObject({ name: "AbortError" });
  expect(expired).not.toHaveBeenCalled();
  expect(getCSRF()).toBe("old");
});

it("does not expire a new login because an old 401 arrived late", async () => {
  const response = deferred<Response>();
  vi.stubGlobal(
    "fetch",
    vi.fn(() => response.promise),
  );
  const expired = vi.fn();
  window.addEventListener("session-expired", expired);
  const old = api("/services");
  expireSession();
  beginSession("new");
  response.resolve(Response.json({ error: "old session" }, { status: 401 }));
  await expect(old).rejects.toBeInstanceOf(SessionCancelledError);
  expect(expired).toHaveBeenCalledTimes(1);
  expect(getCSRF()).toBe("new");
});

it("immediately expires a current protected 401 without waiting for its body", async () => {
  const json = vi.fn(() => new Promise(() => {}));
  const expired = vi.fn();
  window.addEventListener("session-expired", expired);
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => ({ ok: false, status: 401, json })),
  );
  await expect(api("/services")).rejects.toBeInstanceOf(SessionCancelledError);
  expect(expired).toHaveBeenCalledOnce();
  expect(json).not.toHaveBeenCalled();
  expect(getCSRF()).toBe("");
});

it("keeps the current session when logout fails, and invalidates it once on success", async () => {
  vi.stubGlobal(
    "fetch",
    vi
      .fn()
      .mockResolvedValueOnce(
        Response.json({ error: "unavailable" }, { status: 503 }),
      )
      .mockResolvedValueOnce(new Response(null, { status: 204 })),
  );
  const scope = captureSession();
  const expired = vi.fn();
  window.addEventListener("session-expired", expired);
  await expect(logout()).rejects.toThrow("unavailable");
  expect(scope.current()).toBe(true);
  expect(getCSRF()).toBe("old");
  await logout();
  expireSession();
  expect(scope.current()).toBe(false);
  expect(expired).toHaveBeenCalledTimes(1);
});

it("creates a new scope for login but preserves it during normal credential rotation", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json(session)),
  );
  const old = captureSession();
  await login("admin", "password");
  expect(old.current()).toBe(false);
  const active = captureSession();
  acceptSession({ ...session, csrf_token: "rotated" });
  expect(active.current()).toBe(true);
  expect(getCSRF()).toBe("rotated");
});

it("does not let an old mutation recreate cleared query caches or run error/settled callbacks", async () => {
  const client = new QueryClient({
    defaultOptions: { mutations: { gcTime: 0 } },
  });
  const result = deferred<{ secret: string }>();
  const started = deferred<void>();
  const onError = vi.fn();
  const onSettled = vi.fn();
  const observer = new MutationObserver(
    client,
    sessionMutationOptions({
      mutationFn: async () => {
        started.resolve();
        return result.promise;
      },
      onSuccess: (data) => {
        client.setQueryData(["private"], data);
      },
      onError,
      onSettled,
    }),
  );
  const request = observer.mutate(undefined);
  await started.promise;
  expireSession();
  client.removeQueries();
  result.resolve({ secret: "old" });
  await expect(request).rejects.toBeInstanceOf(SessionCancelledError);
  expect(client.getQueryData(["private"])).toBeUndefined();
  expect(onError).not.toHaveBeenCalled();
  expect(onSettled).not.toHaveBeenCalled();
  client.clear();
});

it("does not start a mutation paused in onMutate after the session changes", async () => {
  const client = new QueryClient({
    defaultOptions: { mutations: { gcTime: 0 } },
  });
  const ready = deferred<void>();
  const started = deferred<void>();
  const mutationFn = vi.fn();
  const observer = new MutationObserver(
    client,
    sessionMutationOptions({
      mutationFn,
      onMutate: async () => {
        started.resolve();
        await ready.promise;
      },
    }),
  );
  const request = observer.mutate(undefined);
  await started.promise;
  expireSession();
  ready.resolve();
  await expect(request).rejects.toBeInstanceOf(SessionCancelledError);
  expect(mutationFn).not.toHaveBeenCalled();
  client.clear();
});
