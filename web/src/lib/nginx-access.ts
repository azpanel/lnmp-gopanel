export type NginxAccessEntry = {
  client: string;
  time: string;
  request: string;
  status: string;
  bytes: string;
  referer: string;
  userAgent: string;
};

export const httpStatusGroups = ["1xx", "2xx", "3xx", "4xx", "5xx"] as const;

export function isNginxLog(path: string) {
  return /^\/(?:home\/wwwlogs|usr\/local\/nginx\/logs\/gopanel)\/[^/]+\.log$/.test(
    path,
  );
}

// Quoted fields may contain escaped quotes and backslashes. Keep Nginx's
// hexadecimal escapes as text instead of introducing control characters.
const quoted = /^"((?:[^"\\\r\n]|\\[^\r\n])*)"/;
const unquote = (value: string) => value.replace(/\\(["\\])/g, "$1");
const prefix =
  /^(\S+)[ \t]+\S+[ \t]+\S+[ \t]+\[(\d{2}\/[A-Za-z]{3}\/\d{4}:\d{2}:\d{2}:\d{2} [+-]\d{4})\][ \t]+/;

export function parseNginxAccess(text: string): NginxAccessEntry | null {
  const head = prefix.exec(text);
  if (!head) return null;
  let rest = text.slice(head[0].length);
  const request = quoted.exec(rest);
  if (!request) return null;
  rest = rest.slice(request[0].length);
  const result = /^[ \t]+(\d{3})[ \t]+(\d+|-)(?=[ \t]|$)/.exec(rest);
  if (!result) return null;
  rest = rest.slice(result[0].length).trimStart();
  let referer = "-",
    userAgent = "-";
  if (rest.startsWith('"')) {
    const ref = quoted.exec(rest);
    if (!ref) return null;
    rest = rest.slice(ref[0].length);
    if (!/^[ \t]+/.test(rest)) return null;
    const agent = quoted.exec(rest.trimStart());
    if (!agent) return null;
    const suffix = rest.trimStart().slice(agent[0].length);
    if (suffix && !/^[ \t]/.test(suffix)) return null;
    referer = unquote(ref[1]);
    userAgent = unquote(agent[1]);
  }
  return {
    client: head[1],
    time: head[2],
    request: unquote(request[1]),
    status: result[1],
    bytes: result[2],
    referer,
    userAgent,
  };
}

export function matchesHTTPStatus(
  entry: NginxAccessEntry | null | undefined,
  selected: readonly string[],
) {
  return (
    selected.length === 0 ||
    (!!entry &&
      selected.some(
        (value) =>
          value === entry.status ||
          (httpStatusGroups.some((group) => group === value) &&
            value[0] === entry.status[0]),
      ))
  );
}
