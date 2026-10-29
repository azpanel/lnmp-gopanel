export type LogRule = {
  kind: "keyword" | "regex";
  action: "show" | "hide";
  pattern: string;
  ignore_case: boolean;
  enabled: boolean;
};
export type LogFile = {
  name?: string;
  format?: "login";
  path: string;
  size: number;
  modified: string;
  available: boolean;
  custom?: boolean;
  error?: string;
};
export type LogCatalog = { files: LogFile[]; warnings: string[] };
export type LogLine = {
  offset: string;
  text: string;
  truncated?: boolean;
  partial?: boolean;
  ip_location?: string;
};
export type LogWindow = {
  lines: LogLine[];
  start: string;
  end: string;
  from: string;
  to: string;
  replace_from: string;
  size: number;
  modified: string;
  has_before: boolean;
  has_after: boolean;
  limited: boolean;
};
export type LogRequest = {
  path: string;
  mode: "tail" | "before" | "after" | "range";
  start?: string;
  end?: string;
  rules: LogRule[];
};
const prefix = "log-rules:";
export function loadLogRules(path: string): LogRule[] {
  try {
    const rules: unknown = JSON.parse(
      localStorage.getItem(prefix + path) || "[]",
    );
    if (!Array.isArray(rules) || rules.length > 20) return [];
    return rules.filter(
      (r): r is LogRule =>
        r &&
        (r.kind === "keyword" || r.kind === "regex") &&
        (r.action === "show" || r.action === "hide") &&
        typeof r.pattern === "string" &&
        typeof r.ignore_case === "boolean" &&
        typeof r.enabled === "boolean",
    );
  } catch {
    return [];
  }
}
export function saveLogRules(path: string, rules: LogRule[]) {
  localStorage.setItem(prefix + path, JSON.stringify(rules));
}
export function validateLogRules(rules: LogRule[]): string | undefined {
  if (rules.length > 20) return "最多添加 20 条规则";
  const encoder = new TextEncoder();
  if (rules.some((r) => encoder.encode(r.pattern).length > 256))
    return "每条规则最多 256 字节";
  if (rules.reduce((n, r) => n + encoder.encode(r.pattern).length, 0) > 2048)
    return "规则内容合计最多 2048 字节";
  if (rules.some((r) => r.enabled && !r.pattern))
    return "已启用规则的内容不能为空";
}

// Replace by source offset, including a previously visible partial line that
// no longer matches after more bytes have been appended.
export function mergeLogWindow(
  pages: LogWindow[],
  incoming: LogWindow,
  older = false,
) {
  const from = BigInt(incoming.replace_from),
    to = BigInt(incoming.to);
  const cleaned = pages.map((page) => ({
    ...page,
    lines: page.lines.filter(
      (line) => BigInt(line.offset) < from || BigInt(line.offset) >= to,
    ),
  }));
  const next = older ? [incoming, ...cleaned] : [...cleaned, incoming];
  let evicted = false;
  while (
    next.length > 1 &&
    (next.reduce((n, p) => n + BigInt(p.to) - BigInt(p.from), 0n) >
      2n * 1024n * 1024n ||
      next.reduce((n, p) => n + p.lines.length, 0) > 20000 ||
      next.length > 64)
  ) {
    if (older) next.pop();
    else next.shift();
    evicted = true;
  }
  return { pages: next, evicted };
}
export function logLines(pages: LogWindow[]): LogLine[] {
  const lines = new Map<string, LogLine>();
  for (const page of pages)
    for (const line of page.lines) lines.set(line.offset, line);
  return [...lines.values()].sort((a, b) =>
    BigInt(a.offset) < BigInt(b.offset)
      ? -1
      : BigInt(a.offset) > BigInt(b.offset)
        ? 1
        : 0,
  );
}
