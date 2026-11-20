import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { PanelRelease } from "./types";

const session = {
  username: "admin",
  csrf_token: "csrf",
  expires_at: "2030-01-01",
};
const release: PanelRelease = {
  current_version: "v0.1.29",
  latest_version: "v0.1.30",
  has_update: true,
  changelog: "更新日志",
  release_url: "https://github.com/azpanel/lnmp-gopanel/releases/tag/v0.1.30",
};

function storage() {
  const values = new Map<string, string>();
  return {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => values.set(key, value),
    removeItem: (key: string) => values.delete(key),
  };
}

beforeEach(() => {
  vi.resetModules();
  vi.stubGlobal("window", new EventTarget());
  vi.stubGlobal("localStorage", storage());
  vi.stubGlobal("sessionStorage", storage());
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json(release)),
  );
});
afterEach(() => vi.unstubAllGlobals());

it("checks once after actual login, preserves unread and dismissed results across reloads", async () => {
  let updates = await import("./panel-updates");
  updates.activatePanelUpdates(session);
  expect(fetch).not.toHaveBeenCalled();
  updates.beginPanelUpdates(session);
  updates.activatePanelUpdates(session);
  updates.activatePanelUpdates(session); // Includes React StrictMode's repeated effect.
  await vi.waitFor(() =>
    expect(updates.getPanelUpdates().session?.release).toEqual(release),
  );
  expect(fetch).toHaveBeenCalledTimes(1);
  expect(sessionStorage.getItem("panel-update-session")).not.toContain("csrf");

  vi.resetModules();
  updates = await import("./panel-updates");
  updates.activatePanelUpdates(session);
  expect(updates.getPanelUpdates().session?.release).toEqual(release);
  updates.dismissPanelUpdate();
  vi.resetModules();
  updates = await import("./panel-updates");
  updates.activatePanelUpdates(session);
  expect(updates.getPanelUpdates().session?.dismissed).toBe(true);
  expect(fetch).toHaveBeenCalledTimes(1);
});

it("defaults on, persists disabled state and checks immediately when re-enabled", async () => {
  let updates = await import("./panel-updates");
  expect(updates.getPanelUpdates().enabled).toBe(true);
  updates.setPanelUpdatesEnabled(false);
  vi.resetModules();
  updates = await import("./panel-updates");
  updates.beginPanelUpdates(session);
  updates.activatePanelUpdates(session);
  expect(updates.getPanelUpdates().enabled).toBe(false);
  expect(fetch).not.toHaveBeenCalled();
  updates.setPanelUpdatesEnabled(true);
  await vi.waitFor(() =>
    expect(updates.getPanelUpdates().session?.release).toEqual(release),
  );
  expect(fetch).toHaveBeenCalledTimes(1);
  expect(localStorage.getItem("check-panel-updates")).toBe("true");
});

it("cancels a disabled check and ignores its response even if transport ignores abort", async () => {
  let resolve!: (value: Response) => void;
  let signal!: AbortSignal;
  vi.stubGlobal(
    "fetch",
    vi.fn((_url, options) => {
      signal = options.signal;
      return new Promise<Response>((done) => {
        resolve = done;
      });
    }),
  );
  const updates = await import("./panel-updates");
  updates.beginPanelUpdates(session);
  updates.activatePanelUpdates(session);
  updates.setPanelUpdatesEnabled(false);
  expect(signal.aborted).toBe(true);
  resolve(Response.json(release));
  await new Promise((done) => setTimeout(done, 0));
  expect(updates.getPanelUpdates().session?.release).toBeUndefined();
});

it("clears private results at logout and checks again at the next login", async () => {
  const updates = await import("./panel-updates");
  const scope = await import("./session-scope");
  scope.beginSession("first");
  updates.beginPanelUpdates(session);
  updates.activatePanelUpdates(session);
  await vi.waitFor(() =>
    expect(updates.getPanelUpdates().session?.release).toEqual(release),
  );
  updates.dismissPanelUpdate();
  scope.expireSession();
  expect(updates.getPanelUpdates().session).toBeNull();
  expect(sessionStorage.getItem("panel-update-session")).toBeNull();
  scope.beginSession("second");
  updates.beginPanelUpdates(session);
  updates.activatePanelUpdates(session);
  await vi.waitFor(() =>
    expect(updates.getPanelUpdates().session?.release).toEqual(release),
  );
  expect(updates.getPanelUpdates().session?.dismissed).toBe(false);
  expect(fetch).toHaveBeenCalledTimes(2);
});

it("ignores a response from a logged-out session after another login", async () => {
  let resolveOld!: (value: Response) => void;
  const fetcher = vi
    .fn()
    .mockImplementationOnce(
      () =>
        new Promise<Response>((done) => {
          resolveOld = done;
        }),
    )
    .mockResolvedValueOnce(
      Response.json({ ...release, latest_version: "v0.1.31" }),
    );
  vi.stubGlobal("fetch", fetcher);
  const updates = await import("./panel-updates");
  const scope = await import("./session-scope");
  scope.beginSession("first");
  updates.beginPanelUpdates(session);
  updates.activatePanelUpdates(session);
  scope.expireSession();
  scope.beginSession("second");
  const nextSession = { ...session, expires_at: "2031-01-01" };
  updates.beginPanelUpdates(nextSession);
  updates.activatePanelUpdates(nextSession);
  await vi.waitFor(() =>
    expect(updates.getPanelUpdates().session?.release?.latest_version).toBe(
      "v0.1.31",
    ),
  );
  resolveOld(Response.json(release));
  await new Promise((done) => setTimeout(done, 0));
  expect(updates.getPanelUpdates().session?.release?.latest_version).toBe(
    "v0.1.31",
  );
});

it("preserves the viewed result during credential rotation without another request", async () => {
  let updates = await import("./panel-updates");
  updates.beginPanelUpdates(session);
  updates.activatePanelUpdates(session);
  await vi.waitFor(() =>
    expect(updates.getPanelUpdates().session?.release).toEqual(release),
  );
  updates.dismissPanelUpdate();
  const rotated = {
    ...session,
    expires_at: "2031-01-01",
    csrf_token: "rotated",
  };
  updates.activatePanelUpdates(rotated);
  vi.resetModules();
  updates = await import("./panel-updates");
  updates.activatePanelUpdates(rotated);
  expect(updates.getPanelUpdates().session?.dismissed).toBe(true);
  expect(updates.getPanelUpdates().session?.release).toEqual(release);
  expect(fetch).toHaveBeenCalledTimes(1);
});

it("silently stops after failure and does not retry a check interrupted by reload", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () =>
      Response.json({ error: "rate limited" }, { status: 502 }),
    ),
  );
  let updates = await import("./panel-updates");
  updates.beginPanelUpdates(session);
  updates.activatePanelUpdates(session);
  await vi.waitFor(() => expect(fetch).toHaveBeenCalledTimes(1));
  updates.activatePanelUpdates(session);
  expect(updates.getPanelUpdates().session?.release).toBeUndefined();
  vi.resetModules();
  updates = await import("./panel-updates");
  updates.activatePanelUpdates(session);
  expect(fetch).toHaveBeenCalledTimes(1);
});

it("rejects stale/corrupt stored results and survives unavailable browser storage", async () => {
  sessionStorage.setItem("panel-update-session", "broken json");
  let updates = await import("./panel-updates");
  updates.activatePanelUpdates(session);
  expect(updates.getPanelUpdates().session?.release).toBeUndefined();
  sessionStorage.setItem(
    "panel-update-session",
    JSON.stringify({
      identity: "old-session",
      checked: true,
      dismissed: false,
      release,
    }),
  );
  vi.resetModules();
  updates = await import("./panel-updates");
  updates.activatePanelUpdates(session);
  expect(updates.getPanelUpdates().session?.release).toBeUndefined();
  expect(fetch).not.toHaveBeenCalled();

  const unavailable = {
    getItem() {
      throw Error("unavailable");
    },
    setItem() {
      throw Error("unavailable");
    },
    removeItem() {
      throw Error("unavailable");
    },
  };
  vi.stubGlobal("localStorage", unavailable);
  vi.stubGlobal("sessionStorage", unavailable);
  vi.resetModules();
  updates = await import("./panel-updates");
  updates.beginPanelUpdates(session);
  updates.activatePanelUpdates(session);
  await vi.waitFor(() =>
    expect(updates.getPanelUpdates().session?.release).toEqual(release),
  );
  updates.setPanelUpdatesEnabled(false);
  expect(updates.getPanelUpdates().enabled).toBe(false);
});
