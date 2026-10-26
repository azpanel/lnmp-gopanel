import { useLayoutEffect, useMemo, useRef, useState } from "react";
import type { LogLine } from "@/lib/logs";

const rowHeight = 30;
const overscan = 240;
type Measurement = {
  line: LogLine;
  width: number;
  height: number;
};

function sameLine(a: LogLine, b: LogLine) {
  return (
    a.text === b.text && a.truncated === b.truncated && a.partial === b.partial
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
}: {
  lines: LogLine[];
  follow: boolean;
  onFollow: (value: boolean) => void;
}) {
  const viewport = useRef<HTMLDivElement>(null);
  const rows = useRef(new Map<string, HTMLDivElement>());
  const measurements = useRef(new Map<string, Measurement>());
  const anchor = useRef<{ offset: string; y: number } | null>(null);
  const programmaticScroll = useRef<number | null>(null);
  const [scroll, setScroll] = useState(0);
  const [size, setSize] = useState({ width: 0, height: 480 });
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
        height = node.clientHeight;
      setSize((old) =>
        old.width === width && old.height === height ? old : { width, height },
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

  return (
    <div
      ref={viewport}
      className="log-viewport"
      tabIndex={0}
      aria-label="日志内容"
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
        <div className="log-empty">当前范围没有可显示的日志行</div>
      ) : (
        <div className="log-virtual" style={{ height: total }}>
          {lines.slice(first, last).map((line, i) => (
            <div
              className="log-line"
              key={line.offset}
              ref={(node) => {
                if (node) rows.current.set(line.offset, node);
                else rows.current.delete(line.offset);
              }}
              data-offset={line.offset}
              style={{ transform: `translateY(${layout[first + i]}px)` }}
            >
              <span className="log-offset" title="文件字节偏移">
                {line.offset}
              </span>
              <span className="log-text">
                {line.text}
                {line.truncated && (
                  <span className="log-mark">
                    {" "}
                    …[超长行已截断，按前 64 KiB 匹配]
                  </span>
                )}
                {line.partial && <span className="log-mark"> [尚未换行]</span>}
              </span>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
