import { describe, expect, it } from "vitest";
import { loginLogLabel, parseLoginLog } from "./login-log";

const event = {
  time: "2026-10-29T04:00:00.123Z",
  ip: "114.114.114.114",
  ua: "curl/8.10.1",
  username: "admin",
  result: "failure",
  stage: "password",
  reason: "invalid_credentials",
};

describe("login log parsing", () => {
  it("preserves omitted, empty and escaped passwords as distinct values", () => {
    expect(parseLoginLog(JSON.stringify(event))).toEqual(event);
    for (const password of [
      "",
      'bad\n"password',
      "<script>alert(1)</script>",
    ]) {
      const value = { ...event, password };
      expect(parseLoginLog(JSON.stringify(value))).toEqual(value);
    }
  });

  it("allows empty metadata and unknown outcome codes for older or newer events", () => {
    const value = {
      ...event,
      ip: "",
      ua: "",
      username: "",
      result: "future-result",
      stage: "future-stage",
      reason: "future-reason",
      extra: { ignored: true },
    };
    expect(parseLoginLog(JSON.stringify(value))).toEqual(value);
    expect(loginLogLabel("result", value.result)).toBe("future-result");
    expect(loginLogLabel("stage", value.stage)).toBe("future-stage");
    expect(loginLogLabel("reason", value.reason)).toBe("future-reason");
    expect(loginLogLabel("reason", "toString")).toBe("toString");
  });

  it.each([
    "",
    "broken JSON",
    JSON.stringify(event).slice(0, -1),
    "null",
    "[]",
    "42",
    '"text"',
    "{}",
    JSON.stringify({ ...event, time: "invalid-date" }),
    JSON.stringify({ ...event, ip: 42 }),
    JSON.stringify({ ...event, ua: {} }),
    JSON.stringify({ ...event, result: null }),
    JSON.stringify({ ...event, username: undefined }),
    JSON.stringify({ ...event, password: null }),
    JSON.stringify({ ...event, password: 123 }),
  ])("falls back to raw text for invalid entries: %s", (text) => {
    expect(parseLoginLog(text)).toBeNull();
  });
});
