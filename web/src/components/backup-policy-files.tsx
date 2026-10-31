import { useEffect, useId, useRef, useState } from "react";
import { FilterX, FolderInput } from "lucide-react";
import { PolicyLabel } from "@/components/backup-policy-fields";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { api } from "@/lib/api";
import { captureSession } from "@/lib/session-scope";
import {
  readableBytes,
  type FileSize,
  type FileSource,
} from "@/lib/backup-policies";

// All source rows share two permits; an aborted queued request never starts.
let scans = 0;
const queue: (() => void)[] = [];
async function scanPermit(signal: AbortSignal): Promise<() => void> {
  await new Promise<void>((resolve, reject) => {
    let queued = false;
    const start = () => {
      if (signal.aborted) return;
      scans++;
      signal.removeEventListener("abort", abort);
      resolve();
    };
    const abort = () => {
      if (queued) {
        const i = queue.indexOf(start);
        if (i >= 0) queue.splice(i, 1);
      }
      reject(new DOMException("Aborted", "AbortError"));
    };
    if (signal.aborted) {
      abort();
      return;
    }
    signal.addEventListener("abort", abort, { once: true });
    if (scans < 2) start();
    else {
      queued = true;
      queue.push(start);
    }
  });
  return () => {
    scans--;
    queue.shift()?.();
  };
}
export function BackupPolicyFiles({
  value,
  onChange,
  readOnly,
  active,
}: {
  value: FileSource[];
  onChange: (v: FileSource[]) => void;
  readOnly: boolean;
  active: boolean;
}) {
  const [path, setPath] = useState("");
  const [error, setError] = useState("");
  const [expandedPath, setExpandedPath] = useState("");
  function add() {
    const cleaned = path.trim();
    if (!cleaned.startsWith("/")) {
      setError("请输入绝对路径");
      return;
    }
    if (value.some((s) => s.path === cleaned)) {
      setError("该路径已添加");
      return;
    }
    onChange([...value, { path: cleaned, excludes: [] }]);
    setExpandedPath(cleaned);
    setPath("");
    setError("");
  }
  return (
    <div className="policy-section">
      {!readOnly && (
        <div className="policy-inline">
          <div className="policy-search-field policy-path-input">
            <FolderInput size={16} aria-hidden="true" />
            <Input
              aria-label="添加文件或目录路径"
              placeholder="例如 /home/wwwroot/example.com"
              value={path}
              onChange={(e) => setPath(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") {
                  e.preventDefault();
                  add();
                }
              }}
            />
          </div>
          <Button type="button" variant="outline" onClick={add}>
            添加路径
          </Button>
        </div>
      )}
      {error && (
        <p className="form-error" role="alert">
          {error}
        </p>
      )}
      <p className="small-note">
        填写文件或目录的绝对路径。大小为未压缩逻辑字节，不跟随目录内的符号链接。
      </p>
      {value.map((source, index) => (
        <FileSourceRow
          key={source.path}
          source={source}
          active={active}
          expanded={expandedPath === source.path}
          onExpand={() =>
            setExpandedPath((old) => (old === source.path ? "" : source.path))
          }
          readOnly={readOnly}
          onChange={(next) =>
            onChange(value.map((s, i) => (i === index ? next : s)))
          }
          onRemove={() => onChange(value.filter((_, i) => i !== index))}
        />
      ))}
      {!value.length && (
        <p className="small-note">尚未添加文件来源；可仅配置数据库备份。</p>
      )}
    </div>
  );
}
function FileSourceRow({
  source,
  onChange,
  onRemove,
  expanded,
  onExpand,
  active,
  readOnly,
}: {
  source: FileSource;
  onChange: (v: FileSource) => void;
  onRemove: () => void;
  expanded: boolean;
  onExpand: () => void;
  active: boolean;
  readOnly: boolean;
}) {
  const id = useId();
  const [excludeText, setExcludeText] = useState(source.excludes.join("\n"));
  const [result, setResult] = useState<FileSize>();
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [retry, setRetry] = useState(0);
  const revision = JSON.stringify(source);
  const scope = useRef(captureSession()).current;
  useEffect(() => {
    if (!active) return;
    const controller = new AbortController();
    let release: (() => void) | undefined;
    setBusy(true);
    setError("");
    setResult(undefined);
    const timer = setTimeout(() => {
      void (async () => {
        try {
          release = await scanPermit(controller.signal);
          const data = await api<FileSize>("/backups/files/inspect", {
            method: "POST",
            body: revision,
            signal: controller.signal,
          });
          if (!controller.signal.aborted && scope.current()) setResult(data);
        } catch (e) {
          if (!controller.signal.aborted && scope.current())
            setError((e as Error).message);
        } finally {
          release?.();
          if (!controller.signal.aborted && scope.current()) setBusy(false);
        }
      })();
    }, 450);
    return () => {
      clearTimeout(timer);
      controller.abort();
    };
  }, [revision, active, retry, scope]);
  const directory = !result || result.type === "directory";
  return (
    <div className="policy-file-source">
      <div className="policy-file-summary">
        <div className="policy-file-identity">
          <strong className="font-mono" title={source.path}>
            {source.path}
          </strong>
          <span className="small-note">
            {result?.type === "file"
              ? "文件"
              : result?.type === "directory"
                ? "目录"
                : "正在识别类型"}
            {source.excludes.length
              ? ` · ${source.excludes.length} 条排除规则`
              : ""}
          </span>
        </div>
        <div className="policy-file-size">
          <span>原始大小</span>
          <strong>
            {busy
              ? "统计中…"
              : error
                ? "—"
                : readableBytes(result?.original_bytes)}
          </strong>
        </div>
        <div className="policy-file-size">
          <span>预计备份大小</span>
          <strong>
            {busy
              ? "统计中…"
              : error
                ? "—"
                : readableBytes(result?.included_bytes)}
          </strong>
        </div>
        <div className="policy-file-actions">
          {directory && (
            <Button
              type="button"
              variant="ghost"
              size="sm"
              aria-expanded={expanded}
              aria-controls={id + "-rules"}
              onClick={onExpand}
            >
              {expanded ? "收起规则" : "排除规则"}
            </Button>
          )}
          <Button
            type="button"
            variant="ghost"
            size="sm"
            disabled={busy}
            onClick={() => setRetry((n) => n + 1)}
          >
            重新统计
          </Button>
          {!readOnly && (
            <Button
              type="button"
              variant="ghost"
              size="sm"
              aria-label={`移除 ${source.path}`}
              onClick={onRemove}
            >
              移除
            </Button>
          )}
        </div>
      </div>
      <div className="policy-file-feedback" aria-live="polite">
        {error && (
          <p className="form-error" role="alert">
            {error}
          </p>
        )}
        {result && !result.complete && (
          <p className="small-note" role="status">
            {result.error || "当前为部分统计结果"}
          </p>
        )}
      </div>
      {directory && expanded && (
        <label
          className="policy-file-exclusions"
          id={id + "-rules"}
          htmlFor={id}
        >
          <PolicyLabel icon={FilterX}>
            排除子目录或文件（相对路径，每行一条）
          </PolicyLabel>
          <textarea
            id={id}
            aria-label="排除子目录或文件（相对路径，每行一条）"
            className="policy-textarea font-mono"
            rows={3}
            placeholder={"cache/\nlogs/*.log\n**/node_modules/**"}
            disabled={readOnly}
            value={excludeText}
            onChange={(e) => {
              setExcludeText(e.target.value);
              onChange({
                ...source,
                excludes: e.target.value
                  .split("\n")
                  .filter((line) => line.trim() !== ""),
              });
            }}
          />
        </label>
      )}
    </div>
  );
}
