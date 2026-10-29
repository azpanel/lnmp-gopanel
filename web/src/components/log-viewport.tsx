import { useLayoutEffect, useMemo, useRef, useState } from "react";
import type { LogLine } from "@/lib/logs";
import type { NginxAccessEntry } from "@/lib/nginx-access";
import { loginLogLabel, type LoginLogEntry } from "@/lib/login-log";
import { UserAgentIcon } from "@/components/user-agent-icon";

const rowHeight = 30;
const overscan = 240;
type Measurement = {
  line: LogLine;
  width: number;
  height: number;
};

function sameLine(a: LogLine, b: LogLine) {
  return (
    a.text === b.text &&
    a.truncated === b.truncated &&
    a.partial === b.partial &&
    a.ip_location === b.ip_location
  );
}

function LogText({ line }: { line: LogLine }) {
  return (
    <>
      {line.text}
      {line.truncated && (
        <span className="log-mark"> …[超长行已截断，按前 64 KiB 匹配]</span>
      )}
      {line.partial && <span className="log-mark"> [尚未换行]</span>}
    </>
  );
}

const accessHeaders = [
  "字节偏移",
  "客户端 IP",
  "IP 属地",
  "时间",
  "请求",
  "HTTP 状态码",
  "响应字节数",
  "Referer",
  "User-Agent",
];

const loginHeaders = [
  "字节偏移",
  "时间",
  "客户端 IP",
  "IP 属地",
  "用户名",
  "结果",
  "验证阶段",
  "原因",
  "失败密码",
  "User-Agent",
];

function LoginCells({ entry, line }: { entry: LoginLogEntry; line: LogLine }) {
  return (
    <>
      <span role="cell" title={entry.time}>
        {new Date(entry.time).toLocaleString("zh-CN")}
      </span>
      <span role="cell">{entry.ip || "—"}</span>
      <span role="cell">{line.ip_location || "未知"}</span>
      <span role="cell">{entry.username || "—"}</span>
      <span role="cell" className="log-login-result" data-result={entry.result}>
        {loginLogLabel("result", entry.result)}
      </span>
      <span role="cell">{loginLogLabel("stage", entry.stage)}</span>
      <span role="cell">
        {loginLogLabel("reason", entry.reason)}
        {line.truncated && <span className="log-mark"> [已截断]</span>}
        {line.partial && <span className="log-mark"> [尚未换行]</span>}
      </span>
      <span role="cell">
        {entry.password === undefined
          ? "—"
          : entry.password === ""
            ? "（空密码）"
            : entry.password}
      </span>
      <span role="cell">
        <UserAgentIcon userAgent={entry.ua} />
        {entry.ua || "—"}
      </span>
    </>
  );
}

// Find the row containing a pixel offset in the variable-height layout.
function rowAt(starts: number[], y: number) {
  let low = 0,
    high = starts.length - 1;
  while (low < high) {
    const mid = Math.ceil((low + high) / 2);
    if (starts[mid] <= y) low = mid;
    else high = mid - 1;
  }
  return Math.min(low, Math.max(0, starts.length - 2));
}

export function LogViewport({
  lines,
  follow,
  onFollow,
  accessEntries,
  loginEntries,
  formatted = false,
}: {
  lines: LogLine[];
  follow: boolean;
  onFollow: (value: boolean) => void;
  accessEntries?: ReadonlyMap<string, NginxAccessEntry | null>;
  loginEntries?: ReadonlyMap<string, LoginLogEntry | null>;
  formatted?: boolean;
}) {
  const login = loginEntries !== undefined;
  const headers = login ? loginHeaders : accessHeaders;
  const viewport = useRef<HTMLDivElement>(null);
  const rows = useRef(new Map<string, HTMLDivElement>());
  const measurements = useRef(new Map<string, Measurement>());
  const anchor = useRef<{ offset: string; y: number } | null>(null);
  const programmaticScroll = useRef<number | null>(null);
  const [scroll, setScroll] = useState(0);
  const [size, setSize] = useState({ width: 0, height: 480, gutter: 0 });
  const [revision, setRevision] = useState(0);
  const layout = useMemo(() => {
    const starts = [0];
    for (const line of lines) {
      const cached = measurements.current.get(line.offset);
      const height =
        cached && cached.width === size.width && sameLine(cached.line, line)
          ? cached.height
          : rowHeight;
      starts.push(starts[starts.length - 1] + height);
    }
    return starts;
  }, [lines, size.width, revision]);
  const total = layout[layout.length - 1];
  const first = rowAt(layout, Math.max(0, scroll - overscan));
  const last = Math.min(
    lines.length,
    rowAt(layout, scroll + size.height + overscan) + 1,
  );

  useLayoutEffect(() => {
    const offsets = new Set(lines.map((line) => line.offset));
    for (const offset of measurements.current.keys()) {
      if (!offsets.has(offset)) measurements.current.delete(offset);
    }
  }, [lines]);

  useLayoutEffect(() => {
    const node = viewport.current!;
    const measure = () => {
      const width = node.clientWidth,
        height = node.clientHeight,
        gutter = node.offsetWidth - node.clientWidth;
      setSize((old) =>
        old.width === width && old.height === height && old.gutter === gutter
          ? old
          : { width, height, gutter },
      );
      let changed = false;
      for (let i = first; i < last; i++) {
        const line = lines[i],
          row = rows.current.get(line.offset);
        if (!row) continue;
        const height = row.getBoundingClientRect().height;
        const old = measurements.current.get(line.offset);
        if (
          !old ||
          old.width !== width ||
          old.height !== height ||
          !sameLine(old.line, line)
        ) {
          measurements.current.set(line.offset, { line, width, height });
          changed = true;
        }
      }
      if (changed) setRevision((value) => value + 1);
    };
    const observer = new ResizeObserver(measure);
    observer.observe(node);
    for (const row of rows.current.values()) observer.observe(row);
    measure();
    return () => observer.disconnect();
  }, [lines, first, last, size.width]);

  useLayoutEffect(() => {
    const node = viewport.current!;
    if (follow) node.scrollTop = node.scrollHeight;
    else if (anchor.current) {
      const index = lines.findIndex(
        (line) => line.offset === anchor.current!.offset,
      );
      if (index >= 0) node.scrollTop = layout[index] + anchor.current.y;
    }
    programmaticScroll.current = node.scrollTop;
    setScroll(node.scrollTop);
  }, [lines, layout, follow, size.height]);

  const content = (
    <div
      ref={viewport}
      className="log-viewport"
      tabIndex={0}
      aria-label="日志内容"
      role={formatted ? "rowgroup" : undefined}
      onScroll={(event) => {
        const node = event.currentTarget;
        const index = rowAt(layout, node.scrollTop);
        setScroll(node.scrollTop);
        anchor.current = lines[index]
          ? { offset: lines[index].offset, y: node.scrollTop - layout[index] }
          : null;
        const automatic = programmaticScroll.current === node.scrollTop;
        programmaticScroll.current = null;
        const atBottom =
          node.scrollHeight - node.scrollTop - node.clientHeight < 8;
        if (!automatic && !atBottom && follow) onFollow(false);
      }}
    >
      {lines.length === 0 ? (
        <div className="log-empty" role={formatted ? "row" : undefined}>
          <div
            role={formatted ? "cell" : undefined}
            aria-colspan={formatted ? headers.length : undefined}
          >
            当前范围没有可显示的日志行
          </div>
        </div>
      ) : (
        <div className="log-virtual" style={{ height: total }}>
          {lines.slice(first, last).map((line, i) => {
            const entry = accessEntries?.get(line.offset);
            const loginEntry = loginEntries?.get(line.offset);
            return (
              <div
                className={`log-line${formatted ? " log-access-row" : ""}`}
                role={formatted ? "row" : undefined}
                aria-rowindex={formatted ? first + i + 2 : undefined}
                key={line.offset}
                ref={(node) => {
                  if (node) rows.current.set(line.offset, node);
                  else rows.current.delete(line.offset);
                }}
                data-offset={line.offset}
                style={{ transform: `translateY(${layout[first + i]}px)` }}
              >
                <span
                  className="log-offset"
                  title="文件字节偏移"
                  role={formatted ? "cell" : undefined}
                >
                  {line.offset}
                </span>
                {formatted ? (
                  loginEntry ? (
                    <LoginCells entry={loginEntry} line={line} />
                  ) : entry ? (
                    <>
                      <span role="cell">{entry.client}</span>
                      <span role="cell">{line.ip_location || "未知"}</span>
                      <span role="cell">{entry.time}</span>
                      <span role="cell">
                        {entry.request}
                        {line.truncated && (
                          <span className="log-mark"> [已截断]</span>
                        )}
                        {line.partial && (
                          <span className="log-mark"> [尚未换行]</span>
                        )}
                      </span>
                      <span
                        role="cell"
                        className="log-http-status"
                        data-group={entry.status[0]}
                      >
                        {entry.status}
                      </span>
                      <span role="cell">{entry.bytes}</span>
                      <span role="cell">{entry.referer}</span>
                      <span role="cell">
                        <UserAgentIcon userAgent={entry.userAgent} />
                        {entry.userAgent}
                      </span>
                    </>
                  ) : (
                    <span
                      className="log-access-unparsed log-text"
                      role="cell"
                      aria-colspan={headers.length - 1}
                    >
                      <span className="log-mark">[无法解析] </span>
                      <LogText line={line} />
                    </span>
                  )
                ) : (
                  <span className="log-text">
                    {entry && <UserAgentIcon userAgent={entry.userAgent} />}
                    <LogText line={line} />
                  </span>
                )}
              </div>
            );
          })}
        </div>
      )}
    </div>
  );

  if (!formatted) return content;
  return (
    <div
      className="log-table-scroll"
      tabIndex={0}
      aria-label={login ? "登录日志表格横向滚动" : "访问日志表格横向滚动"}
    >
      <div
        className={`log-access-table${login ? " log-login-table" : ""}`}
        role="table"
        aria-label={login ? "登录日志" : "Nginx 访问日志"}
        aria-rowcount={lines.length + 1}
        aria-colcount={headers.length}
      >
        <div role="rowgroup">
          <div
            className="log-access-header"
            role="row"
            aria-rowindex={1}
            style={{ paddingRight: size.gutter }}
          >
            {headers.map((name) => (
              <span role="columnheader" key={name}>
                {name}
              </span>
            ))}
          </div>
        </div>
        {content}
      </div>
    </div>
  );
}
