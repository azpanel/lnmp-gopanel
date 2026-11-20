import { api } from "./api";
import { captureSession, onSessionChange } from "./session-scope";
import type { PanelRelease, Session } from "./types";

const preferenceKey = "check-panel-updates";
const resultKey = "panel-update-session";
type UpdateSession = {
  identity: string;
  checked: boolean;
  dismissed: boolean;
  release?: PanelRelease;
};
type Snapshot = { enabled: boolean; session: UpdateSession | null };

function readEnabled() {
  try {
    return localStorage.getItem(preferenceKey) !== "false";
  } catch {
    return true;
  }
}

let snapshot: Snapshot = { enabled: readEnabled(), session: null };
let request: AbortController | null = null;
const listeners = new Set<() => void>();

function identity(session: Session) {
  // Persist a non-secret session identity, never the cookie or CSRF token.
  return JSON.stringify([session.username, session.expires_at]);
}

function publish(session: UpdateSession | null, enabled = snapshot.enabled) {
  snapshot = { enabled, session };
  try {
    if (session) sessionStorage.setItem(resultKey, JSON.stringify(session));
    else sessionStorage.removeItem(resultKey);
  } catch {
    // Updates remain usable in memory when browser storage is unavailable.
  }
  for (const listener of listeners) listener();
}

function cancel() {
  request?.abort();
  request = null;
}

onSessionChange(() => {
  cancel();
  publish(null);
});

export function beginPanelUpdates(session: Session) {
  cancel();
  publish({ identity: identity(session), checked: false, dismissed: false });
}

function restore(session: Session): UpdateSession | null {
  try {
    const saved = JSON.parse(sessionStorage.getItem(resultKey) ?? "null");
    if (
      saved?.identity !== identity(session) ||
      typeof saved.checked !== "boolean" ||
      typeof saved.dismissed !== "boolean"
    )
      return null;
    const release = saved.release;
    if (
      release !== undefined &&
      (typeof release.current_version !== "string" ||
        typeof release.latest_version !== "string" ||
        typeof release.has_update !== "boolean" ||
        typeof release.changelog !== "string" ||
        typeof release.release_url !== "string" ||
        !release.release_url.startsWith(
          "https://github.com/azpanel/lnmp-gopanel/releases/tag/",
        ))
    )
      return null;
    // Restoring a page must never retry an unfinished login check.
    return { ...saved, checked: true };
  } catch {
    return null;
  }
}

function check() {
  const session = snapshot.session;
  if (!snapshot.enabled || !session || session.checked) return;
  cancel();
  const controller = new AbortController();
  const scope = captureSession();
  request = controller;
  publish({ ...session, checked: true, dismissed: false, release: undefined });
  void api<PanelRelease>("/panel/release", { signal: controller.signal })
    .then((release) => {
      if (request !== controller || !scope.current() || !snapshot.session)
        return;
      publish({ ...snapshot.session, release });
    })
    .catch(() => {
      // A background check must not interrupt login or show an error toast.
    })
    .finally(() => {
      if (request === controller) request = null;
    });
}

export function activatePanelUpdates(session: Session) {
  const key = identity(session);
  if (snapshot.session?.identity !== key) {
    // Credential changes can rotate the authenticated session during this visit.
    if (snapshot.session) publish({ ...snapshot.session, identity: key });
    else
      publish(
        restore(session) ?? { identity: key, checked: true, dismissed: false },
      );
  }
  check();
}

export function setPanelUpdatesEnabled(enabled: boolean) {
  if (enabled === snapshot.enabled) return;
  cancel();
  try {
    localStorage.setItem(preferenceKey, String(enabled));
  } catch {
    // Keep the switch functional without persistent storage.
  }
  publish(
    snapshot.session
      ? { ...snapshot.session, checked: !enabled, release: undefined }
      : null,
    enabled,
  );
  if (enabled) check();
}

export function dismissPanelUpdate() {
  if (snapshot.session) publish({ ...snapshot.session, dismissed: true });
}

export function getPanelUpdates() {
  return snapshot;
}

export function subscribePanelUpdates(listener: () => void) {
  listeners.add(listener);
  return () => listeners.delete(listener);
}
