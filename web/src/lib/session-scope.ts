let epoch = 0;
let ended = false;
let csrf = "";
const requests = new Set<AbortController>();
const listeners = new Set<() => void>();

export class SessionCancelledError extends Error {
  constructor() {
    super("会话已变更，已忽略旧请求");
    this.name = "SessionCancelledError";
  }
}

export function captureSession() {
  const captured = epoch;
  return {
    current: () => captured === epoch,
    assert: () => {
      if (captured !== epoch) throw new SessionCancelledError();
    },
  };
}

function advance() {
  epoch++;
  for (const controller of requests) controller.abort();
  requests.clear();
  for (const listener of listeners) listener();
}

export function expireSession() {
  if (ended) return;
  ended = true;
  csrf = "";
  advance();
  window.dispatchEvent(new Event("session-expired"));
}

export function beginSession(token: string) {
  advance();
  ended = false;
  csrf = token;
}

export function setCSRF(token: string) {
  csrf = token;
  ended = false;
}

export function getCSRF() {
  return csrf;
}

export function trackRequest(controller: AbortController) {
  requests.add(controller);
  return () => requests.delete(controller);
}

export function onSessionChange(listener: () => void) {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}
