export type LoginLogEntry = {
  time: string;
  ip: string;
  ua: string;
  username: string;
  result: string;
  stage: string;
  reason: string;
  password?: string;
};

const fields = ["time", "ip", "ua", "username", "result", "stage", "reason"];

export function parseLoginLog(text: string): LoginLogEntry | null {
  try {
    const value = JSON.parse(text);
    if (
      !value ||
      typeof value !== "object" ||
      Array.isArray(value) ||
      fields.some((field) => typeof value[field] !== "string") ||
      !Number.isFinite(Date.parse(value.time)) ||
      (value.password !== undefined && typeof value.password !== "string")
    ) {
      return null;
    }
    return value as LoginLogEntry;
  } catch {
    return null;
  }
}

const labels: Record<"result" | "stage" | "reason", Record<string, string>> = {
  result: { success: "成功", failure: "失败" },
  stage: { password: "密码验证", totp: "二步验证" },
  reason: {
    authenticated: "认证通过",
    invalid_credentials: "用户名或密码错误",
    invalid_second_factor: "二步验证失败或挑战已失效",
    invalid_request: "参数无效",
    rate_limited: "尝试次数过多",
    authentication_unavailable: "认证状态不可用",
  },
};

export function loginLogLabel(
  kind: "result" | "stage" | "reason",
  value: string,
) {
  return Object.hasOwn(labels[kind], value)
    ? labels[kind][value]
    : value || "—";
}
