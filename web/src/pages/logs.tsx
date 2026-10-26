import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { FileText, Pause, Play, Plus, RefreshCw, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { api } from "@/lib/api";
import { bytes } from "@/lib/format";
import { loadLogRules, logLines, validateLogRules } from "@/lib/logs";
import type { LogCatalog, LogLine, LogRule } from "@/lib/logs";
import { useLogReader } from "@/lib/use-log-reader";

const rowHeight = 26;
function LogViewport({
  lines,
  follow,
  onFollow,
}: {
  lines: LogLine[];
  follow: boolean;
  onFollow: (value: boolean) => void;
}) {
  const viewport = useRef<HTMLDivElement>(null);
  const anchor = useRef<{ offset: string; y: number } | null>(null);
  const [scroll, setScroll] = useState(0),
    [height, setHeight] = useState(480);
  useEffect(() => {
    const node = viewport.current!;
    const observer = new ResizeObserver(() => setHeight(node.clientHeight));
    observer.observe(node);
    return () => observer.disconnect();
  }, []);
  useLayoutEffect(() => {
    const node = viewport.current!;
    if (follow) node.scrollTop = node.scrollHeight;
    else if (anchor.current) {
      const index = lines.findIndex(
        (line) => line.offset === anchor.current!.offset,
      );
      if (index >= 0) node.scrollTop = index * rowHeight + anchor.current.y;
    }
    setScroll(node.scrollTop);
  }, [lines, follow]);
  const first = Math.max(0, Math.floor(scroll / rowHeight) - 8);
  const last = Math.min(
    lines.length,
    Math.ceil((scroll + height) / rowHeight) + 8,
  );
  return (
    <div
      ref={viewport}
      className="log-viewport"
      tabIndex={0}
      aria-label="日志内容"
      onScroll={(event) => {
        const node = event.currentTarget,
          index = Math.floor(node.scrollTop / rowHeight);
        setScroll(node.scrollTop);
        anchor.current = lines[index]
          ? {
              offset: lines[index].offset,
              y: node.scrollTop - index * rowHeight,
            }
          : null;
        const atBottom =
          node.scrollHeight - node.scrollTop - node.clientHeight < 8;
        if (!atBottom && follow) onFollow(false);
      }}
    >
      {lines.length === 0 ? (
        <div className="log-empty">当前范围没有可显示的日志行</div>
      ) : (
        <div
          className="log-virtual"
          style={{ height: lines.length * rowHeight }}
        >
          <div style={{ paddingTop: first * rowHeight }}>
            {lines.slice(first, last).map((line) => (
              <div
                className="log-line"
                key={line.offset}
                style={{ height: rowHeight }}
              >
                <span className="log-offset" title="文件字节偏移">
                  {line.offset}
                </span>
                <span>
                  {line.text}
                  {line.truncated && (
                    <span className="log-mark">
                      {" "}
                      …[超长行已截断，按前 64 KiB 匹配]
                    </span>
                  )}
                  {line.partial && (
                    <span className="log-mark"> [尚未换行]</span>
                  )}
                </span>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

export function Logs() {
  const catalog = useQuery({
    queryKey: ["logs"],
    queryFn: ({ signal }) => api<LogCatalog>("/logs", { signal }),
    retry: false,
    refetchOnWindowFocus: false,
  });
  const [selected, setSelected] = useState("");
  const files = useMemo(
    () =>
      [...(catalog.data?.files || [])].sort((a, b) =>
        a.path.localeCompare(b.path),
      ),
    [catalog.data],
  );
  const path =
    selected ||
    files.find((file) => file.available)?.path ||
    files[0]?.path ||
    "";
  const file = files.find((file) => file.path === path);
  const reader = useLogReader(file?.available ? path : "");
  const [rules, setRules] = useState<LogRule[]>([]);
  const [dirty, setDirty] = useState(false);
  const rulesRef = useRef(rules);
  rulesRef.current = rules;
  useEffect(() => {
    setRules(loadLogRules(path));
    setDirty(false);
  }, [path]);
  const lines = useMemo(() => logLines(reader.pages), [reader.pages]);
  const first = reader.pages[0],
    last = reader.pages.at(-1);
  const patchRule = (index: number, patch: Partial<LogRule>) => {
    setRules((old) =>
      old.map((rule, i) => (i === index ? { ...rule, ...patch } : rule)),
    );
    setDirty(true);
    reader.setRuleError("");
  };
  const apply = async () => {
    const error = validateLogRules(rules);
    if (error) {
      reader.setRuleError(error);
      return;
    }
    const snapshot = JSON.stringify(rules);
    if (await reader.apply(rules)) {
      if (JSON.stringify(rulesRef.current) === snapshot) setDirty(false);
    }
  };
  return (
    <>
      <div className="page-heading">
        <div>
          <div className="eyebrow muted">LOG VIEWER</div>
          <h1>日志读取</h1>
          <p>分批浏览历史日志，实时跟踪新增内容。</p>
        </div>
        <Button
          variant="outline"
          size="sm"
          disabled={catalog.isFetching}
          onClick={() => void catalog.refetch()}
        >
          <RefreshCw
            size={14}
            className={catalog.isFetching ? "animate-spin" : ""}
          />
          刷新文件
        </Button>
      </div>
      {catalog.error && (
        <div className="notice" role="alert">
          日志列表读取失败：{catalog.error.message}
        </div>
      )}
      {catalog.data?.warnings.map((warning) => (
        <div className="notice" key={warning}>
          {warning}
        </div>
      ))}
      <Card>
        <CardHeader>
          <CardTitle className="card-title-icon">
            <FileText size={17} />
            日志文件
          </CardTitle>
        </CardHeader>
        <CardContent>
          <label className="log-file-label" htmlFor="log-file">
            选择日志
          </label>
          <select
            id="log-file"
            className="logs-select log-file-select"
            value={path}
            onChange={(e) => setSelected(e.target.value)}
            disabled={!files.length}
          >
            {!files.length && (
              <option value="">
                {catalog.isPending ? "正在读取文件列表…" : "没有可读取的日志"}
              </option>
            )}
            {path && !file && files.length > 0 && (
              <option value={path}>{path}（文件已不在列表中）</option>
            )}
            {files.map((file) => (
              <option key={file.path} value={file.path}>
                {file.path}
                {!file.available ? "（不可读取）" : ""}
              </option>
            ))}
          </select>
          {path && !file && !catalog.isPending && (
            <p className="notice" role="alert">
              所选日志已不在文件列表中，请重新选择文件。
            </p>
          )}
          {file && (
            <p className="small-note log-meta">
              {file.error ||
                `${bytes(last?.size ?? file.size)} · 修改时间 ${new Date(last?.modified || file.modified).toLocaleString("zh-CN")}`}
            </p>
          )}
        </CardContent>
      </Card>
      <Card className="log-card">
        <CardHeader className="log-card-header">
          <CardTitle>显示与隐藏规则</CardTitle>
          <div className="log-actions">
            <Button
              size="sm"
              variant="outline"
              disabled={rules.length >= 20 || !path}
              onClick={() => {
                setRules([
                  ...rules,
                  {
                    kind: "keyword",
                    action: "show",
                    pattern: "",
                    ignore_case: false,
                    enabled: true,
                  },
                ]);
                setDirty(true);
              }}
            >
              {" "}
              <Plus size={14} />
              添加规则
            </Button>
            <Button
              size="sm"
              disabled={!file?.available || reader.busy || !dirty}
              onClick={() => void apply()}
            >
              应用规则
            </Button>
          </div>
        </CardHeader>
        <CardContent>
          <p className="small-note">
            逐行匹配已加载范围。显示规则命中任意一条即可，隐藏规则优先；没有显示规则时默认显示全部。
          </p>
          <div className="log-rules">
            {rules.map((rule, index) => (
              <div className="log-rule" key={index}>
                <label className="log-check">
                  <input
                    type="checkbox"
                    checked={rule.enabled}
                    onChange={(e) =>
                      patchRule(index, { enabled: e.target.checked })
                    }
                    aria-label={`启用规则 ${index + 1}`}
                  />
                  启用
                </label>
                <select
                  className="logs-select"
                  aria-label={`规则 ${index + 1} 类型`}
                  value={rule.kind}
                  onChange={(e) =>
                    patchRule(index, {
                      kind: e.target.value as LogRule["kind"],
                    })
                  }
                >
                  <option value="keyword">关键词</option>
                  <option value="regex">正则</option>
                </select>
                <Input
                  className="log-rule-pattern"
                  aria-label={`规则 ${index + 1} 内容`}
                  placeholder={
                    rule.kind === "regex"
                      ? "Go 正则，例如 error|warn"
                      : "包含关键词"
                  }
                  value={rule.pattern}
                  onChange={(e) =>
                    patchRule(index, { pattern: e.target.value })
                  }
                />
                <select
                  className="logs-select"
                  aria-label={`规则 ${index + 1} 操作`}
                  value={rule.action}
                  onChange={(e) =>
                    patchRule(index, {
                      action: e.target.value as LogRule["action"],
                    })
                  }
                >
                  <option value="show">显示匹配</option>
                  <option value="hide">隐藏匹配</option>
                </select>
                <label className="log-check">
                  <input
                    type="checkbox"
                    checked={rule.ignore_case}
                    onChange={(e) =>
                      patchRule(index, { ignore_case: e.target.checked })
                    }
                  />
                  忽略大小写
                </label>
                <Button
                  size="icon"
                  variant="ghost"
                  aria-label={`删除规则 ${index + 1}`}
                  onClick={() => {
                    setRules(rules.filter((_, i) => i !== index));
                    setDirty(true);
                    reader.setRuleError("");
                  }}
                >
                  <Trash2 size={15} />
                </Button>
              </div>
            ))}
          </div>
          {reader.ruleError && (
            <div className="notice" role="alert">
              {reader.ruleError}
            </div>
          )}
          <p className="small-note">
            规则按文件保存在当前浏览器。正则使用 Go
            语法，不支持回溯引用和前后查找。{dirty && " 当前修改尚未应用。"}
          </p>
        </CardContent>
      </Card>
      {reader.error && (
        <div className="notice" role="alert">
          {reader.error}
        </div>
      )}
      {reader.changed && !reader.error && (
        <div className="notice" role="status">
          日志已轮转或截断，当前已切换至新文件的最新内容。
        </div>
      )}
      <Card className="log-card">
        <CardHeader className="log-card-header">
          <CardTitle>日志内容</CardTitle>
          <div className="log-actions">
            <Button
              size="sm"
              variant="outline"
              disabled={reader.busy || !first?.has_before}
              onClick={() => void reader.older()}
            >
              加载更早日志
            </Button>
            <Button
              size="sm"
              variant="outline"
              disabled={!file?.available || reader.busy}
              onClick={() => reader.setPaused(!reader.paused)}
            >
              {reader.paused ? <Play size={14} /> : <Pause size={14} />}
              {reader.paused ? "继续追踪" : "暂停追踪"}
            </Button>
            <Button
              size="sm"
              variant="outline"
              disabled={!file?.available || reader.busy}
              onClick={() => void reader.latest()}
            >
              回到最新
            </Button>
          </div>
        </CardHeader>
        <CardContent>
          <div className="log-status">
            <span role="status">
              {reader.busy
                ? "读取中…"
                : reader.paused
                  ? "已暂停追踪"
                  : reader.error
                    ? "等待重试"
                    : file?.available
                      ? "实时追踪 · 约每秒更新"
                      : "尚未读取"}
              {last?.has_after ? " · 正在分批追赶新增内容" : ""}
            </span>
            <label className="log-check">
              <input
                type="checkbox"
                checked={reader.follow}
                onChange={(e) => reader.setFollow(e.target.checked)}
              />
              自动滚动
            </label>
          </div>
          <LogViewport
            key={path}
            lines={lines}
            follow={reader.follow}
            onFollow={reader.setFollow}
          />
          <p className="small-note log-meta">
            {first && last
              ? `字节范围 ${first.from}–${last.to} · ${lines.length} 行可显示内容。`
              : "选择文件后自动读取末尾日志。"}{" "}
            历史按需加载，不搜索整个文件；慢 SQL 和错误堆栈也逐行筛选。
          </p>
          {(reader.evicted || reader.pages.some((p) => p.limited)) && (
            <p className="small-note">
              {reader.evicted && "已释放另一端的缓存，可按需重新加载。"}
              {reader.pages.some((p) => p.limited) &&
                "超长行仅展示并匹配前 64 KiB，起点在当前范围外的超长行可能跳过。"}
            </p>
          )}
        </CardContent>
      </Card>
    </>
  );
}
