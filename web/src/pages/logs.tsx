import { useEffect, useMemo, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  AlertTriangle,
  ArrowDownToLine,
  FileText,
  CircleHelp,
  Eye,
  EyeOff,
  History,
  Info,
  ListFilter,
  Pause,
  Play,
  Plus,
  RefreshCw,
  Regex,
  TextSearch,
  Trash2,
  X,
} from "lucide-react";
import { Tooltip } from "radix-ui";
import { Button } from "@/components/ui/button";
import { LogViewport } from "@/components/log-viewport";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { api } from "@/lib/api";
import { bytes } from "@/lib/format";
import { loadLogRules, logLines, validateLogRules } from "@/lib/logs";
import type { LogCatalog, LogFile, LogRule } from "@/lib/logs";
import { useLogReader } from "@/lib/use-log-reader";
import { recordLogRender } from "@/lib/log-performance";
import { parseLoginLog } from "@/lib/login-log";
import {
  httpStatusGroups,
  isNginxLog,
  matchesHTTPStatus,
  parseNginxAccess,
} from "@/lib/nginx-access";

function LogLocationHelp() {
  const [open, setOpen] = useState(false);
  return (
    <Tooltip.Provider delayDuration={150}>
      <Tooltip.Root open={open} onOpenChange={setOpen}>
        <Tooltip.Trigger asChild>
          <Button
            type="button"
            variant="ghost"
            size="icon"
            className="log-location-help"
            aria-label="IP 属地说明"
            onClick={(event) => {
              event.preventDefault();
              setOpen(true);
            }}
          >
            <CircleHelp size={15} aria-hidden="true" />
          </Button>
        </Tooltip.Trigger>
        <Tooltip.Portal>
          <Tooltip.Content
            className="log-location-tooltip"
            aria-hidden={!open}
            side="top"
            sideOffset={6}
            collisionPadding={16}
          >
            IP 地址位置数据由 纯真 CZ88 提供 · 数据版本 20261021 · IPv6 暂不支持
            <Tooltip.Arrow className="log-location-arrow" />
          </Tooltip.Content>
        </Tooltip.Portal>
      </Tooltip.Root>
    </Tooltip.Provider>
  );
}

export function Logs() {
  const client = useQueryClient();
  const [addOpen, setAddOpen] = useState(false);
  const [customPath, setCustomPath] = useState("");
  const [addError, setAddError] = useState("");
  const catalog = useQuery({
    queryKey: ["logs"],
    queryFn: ({ signal }) => api<LogCatalog>("/logs", { signal }),
    retry: false,
    refetchOnWindowFocus: false,
  });
  const [selected, setSelected] = useState("");
  const add = useMutation({
    mutationFn: (path: string) =>
      api<LogFile>("/logs/custom", {
        method: "POST",
        body: JSON.stringify({ path }),
      }),
    onSuccess: async (file) => {
      await client.cancelQueries({ queryKey: ["logs"] });
      client.setQueryData<LogCatalog>(["logs"], (old) => ({
        files: [
          ...(old?.files ?? []).filter((item) => item.path !== file.path),
          file,
        ],
        warnings: old?.warnings ?? [],
      }));
      setSelected(file.path);
      setAddOpen(false);
      setCustomPath("");
      void client.invalidateQueries({ queryKey: ["logs"] });
    },
    onError: (error) => setAddError(error.message),
  });
  const remove = useMutation({
    mutationFn: (path: string) =>
      api<void>("/logs/custom", {
        method: "DELETE",
        body: JSON.stringify({ path }),
      }),
    onSuccess: async (_, removedPath) => {
      await client.cancelQueries({ queryKey: ["logs"] });
      client.setQueryData<LogCatalog>(["logs"], (old) => ({
        files: (old?.files ?? []).filter((item) => item.path !== removedPath),
        warnings: old?.warnings ?? [],
      }));
      setSelected("");
      void client.invalidateQueries({ queryKey: ["logs"] });
    },
  });
  const [rulesOpen, setRulesOpen] = useState(false);
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
  const [table, setTable] = useState(false);
  const [statusFilters, setStatusFilters] = useState<string[]>([]);
  const rulesRef = useRef(rules);
  rulesRef.current = rules;
  useEffect(() => {
    setRules(loadLogRules(path));
    setDirty(false);
    setTable(false);
    setStatusFilters([]);
  }, [path]);
  const lines = useMemo(() => logLines(reader.pages), [reader.pages]);
  useEffect(() => recordLogRender(reader.pages), [reader.pages]);
  const login = file?.format === "login";
  const nginx = !login && isNginxLog(path);
  const formatted = table && (nginx || login);
  const loginEntries = useMemo(
    () =>
      login
        ? new Map(lines.map((line) => [line.offset, parseLoginLog(line.text)]))
        : undefined,
    [login, lines],
  );
  const accessEntries = useMemo(
    () =>
      nginx
        ? new Map(
            lines.map((line) => [line.offset, parseNginxAccess(line.text)]),
          )
        : undefined,
    [nginx, lines],
  );
  const statusCodes = useMemo(
    () =>
      [
        ...new Set([
          ...Array.from(accessEntries?.values() ?? []).flatMap((entry) =>
            entry ? [entry.status] : [],
          ),
          ...statusFilters.filter((value) => /^\d{3}$/.test(value)),
        ]),
      ].sort(),
    [accessEntries, statusFilters],
  );
  const visibleLines = useMemo(
    () =>
      formatted && nginx
        ? lines.filter((line) =>
            matchesHTTPStatus(accessEntries?.get(line.offset), statusFilters),
          )
        : lines,
    [formatted, nginx, lines, accessEntries, statusFilters],
  );
  const first = reader.pages[0],
    last = reader.pages.at(-1);
  const status = reader.paused
    ? "已暂停追踪"
    : reader.error
      ? "等待重试"
      : !file?.available
        ? "尚未读取"
        : last?.has_after
          ? "正在分批追赶新增内容"
          : "";
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
      if (JSON.stringify(rulesRef.current) === snapshot) {
        setDirty(false);
        setRulesOpen(false);
      }
    }
  };
  return (
    <Dialog open={rulesOpen} onOpenChange={setRulesOpen}>
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
      {reader.error && (
        <div className="notice" role="alert">
          {reader.error}
        </div>
      )}
      {remove.error && (
        <div className="notice" role="alert">
          移除日志失败：{remove.error.message}
        </div>
      )}
      {reader.changed && !reader.error && (
        <div className="notice" role="status">
          日志已轮转或截断，当前已切换至新文件的最新内容。
        </div>
      )}
      <Card className="log-card">
        <CardHeader className="log-card-header">
          <div className="log-heading">
            <CardTitle className="card-title-icon">
              <FileText size={17} />
              日志内容
            </CardTitle>
            <label className="sr-only" htmlFor="log-file">
              选择日志
            </label>
            <Select
              value={files.length ? path : ""}
              onValueChange={setSelected}
              disabled={!files.length}
            >
              <SelectTrigger
                id="log-file"
                className="log-file-select"
                title={path || undefined}
              >
                <SelectValue
                  placeholder={
                    catalog.isPending ? "选择日志文件" : "没有可读取的日志"
                  }
                />
              </SelectTrigger>
              <SelectContent>
                {path && !file && files.length > 0 && (
                  <SelectItem value={path}>
                    {path}（文件已不在列表中）
                  </SelectItem>
                )}
                {files.map((file) => (
                  <SelectItem key={file.path} value={file.path}>
                    {file.name ? `${file.name}（${file.path}）` : file.path}
                    {!file.available ? "（不可读取）" : ""}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="log-actions">
            <Dialog
              open={addOpen}
              onOpenChange={(open) => {
                if (add.isPending) return;
                setAddOpen(open);
                if (open) setAddError("");
              }}
            >
              <DialogTrigger asChild>
                <Button variant="outline" size="sm">
                  <Plus size={14} />
                  添加日志
                </Button>
              </DialogTrigger>
              <DialogContent>
                <form
                  className="custom-log-form"
                  onSubmit={(event) => {
                    event.preventDefault();
                    const path = customPath.trim();
                    if (!path.startsWith("/")) {
                      setAddError("请输入日志文件的绝对路径");
                      return;
                    }
                    if (!/\.(log|txt)$/i.test(path)) {
                      setAddError("自定义日志仅支持 .log 或 .txt 文件");
                      return;
                    }
                    setAddError("");
                    add.mutate(path);
                  }}
                >
                  <DialogHeader>
                    <DialogTitle>添加日志文件</DialogTitle>
                    <DialogDescription className="sr-only">
                      输入绝对路径以添加服务器上已有的 .log 或 .txt 日志文件。
                    </DialogDescription>
                  </DialogHeader>
                  <div className="custom-log-alerts">
                    <div className="custom-log-alert" role="alert">
                      <Info size={16} aria-hidden="true" />
                      <p>
                        输入服务器上已有 .log 或 .txt
                        文件的绝对路径，扩展名不区分大小写。添加后保存在服务器上，可继续实时追踪与筛选。
                      </p>
                    </div>
                    <div
                      className="custom-log-alert custom-log-alert-warning"
                      role="alert"
                    >
                      <AlertTriangle size={16} aria-hidden="true" />
                      <p>
                        系统配置、面板数据、凭据与证书、数据库数据等敏感路径禁止读取；不支持目录、符号链接、硬链接或二进制文件。
                      </p>
                    </div>
                  </div>
                  <div className="custom-log-input">
                    <Label htmlFor="custom-log-path">日志文件绝对路径</Label>
                    <Input
                      id="custom-log-path"
                      placeholder="/var/log/application.log"
                      autoComplete="off"
                      required
                      maxLength={2048}
                      disabled={add.isPending}
                      value={customPath}
                      onChange={(event) => setCustomPath(event.target.value)}
                    />
                    {addError && (
                      <p role="alert" className="notice">
                        {addError}
                      </p>
                    )}
                  </div>
                  <DialogFooter>
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      disabled={add.isPending}
                      onClick={() => setAddOpen(false)}
                    >
                      取消
                    </Button>
                    <Button
                      type="submit"
                      size="sm"
                      disabled={add.isPending || !customPath.trim()}
                    >
                      {add.isPending ? "正在添加…" : "添加并读取"}
                    </Button>
                  </DialogFooter>
                </form>
              </DialogContent>
            </Dialog>
            {file?.custom && (
              <Button
                variant="outline"
                size="sm"
                disabled={remove.isPending}
                title="仅从自定义列表移除，不删除磁盘上的文件"
                onClick={() => remove.mutate(path)}
              >
                <Trash2 size={14} />
                移除日志
              </Button>
            )}
            <Button
              variant="outline"
              size="sm"
              disabled={catalog.isFetching}
              onClick={() => void catalog.refetch()}
            >
              <RefreshCw size={14} />
              刷新文件
            </Button>
            <DialogTrigger asChild>
              <Button size="sm" variant="outline">
                <ListFilter size={14} />
                筛选规则{dirty && <span className="small-note">未应用</span>}
              </Button>
            </DialogTrigger>
            <Button
              size="sm"
              variant="outline"
              disabled={reader.busy || !first?.has_before}
              onClick={() => void reader.older()}
            >
              <History size={14} aria-hidden="true" />
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
              <ArrowDownToLine size={14} aria-hidden="true" />
              回到最新
            </Button>
          </div>
        </CardHeader>
        <CardContent>
          {path && !file && !catalog.isPending && (
            <p className="notice" role="alert">
              所选日志已不在文件列表中，请重新选择文件。
            </p>
          )}
          <div className="log-status">
            {file && (
              <p className="small-note log-file-meta">
                {file.error ||
                  `${bytes(last?.size ?? file.size)} · 修改时间 ${new Date(last?.modified || file.modified).toLocaleString("zh-CN")}`}
              </p>
            )}
            <div className="log-reader-controls">
              <label className="log-check">
                <Checkbox
                  checked={reader.follow}
                  onCheckedChange={(checked) =>
                    reader.setFollow(checked === true)
                  }
                />
                自动滚动
              </label>
              {(nginx || login) && (
                <div className="log-format-controls">
                  <label className="log-check">
                    <Checkbox
                      checked={formatted}
                      onCheckedChange={(checked) => {
                        setTable(checked === true);
                        setStatusFilters([]);
                      }}
                    />
                    格式化为表格
                  </label>
                  <LogLocationHelp />
                </div>
              )}
            </div>
          </div>
          <span className="log-reader-status" role="status">
            {status}
          </span>
          {formatted && nginx && (
            <fieldset className="log-http-filters">
              <legend>HTTP 状态码</legend>
              <div className="log-http-options">
                {[...httpStatusGroups, ...statusCodes].map((value) => (
                  <label className="log-check" key={value}>
                    <Checkbox
                      checked={statusFilters.includes(value)}
                      onCheckedChange={(checked) =>
                        setStatusFilters((old) =>
                          checked === true
                            ? [...old, value]
                            : old.filter((item) => item !== value),
                        )
                      }
                    />
                    {value}
                  </label>
                ))}
                <Button
                  size="sm"
                  variant="outline"
                  disabled={statusFilters.length === 0}
                  onClick={() => setStatusFilters([])}
                >
                  清除筛选
                </Button>
              </div>
              <p className="small-note" role="status">
                仅筛选已加载范围 · 显示 {visibleLines.length} / {lines.length}{" "}
                行 · 未选择时显示全部，多个选择取并集。
              </p>
            </fieldset>
          )}
          <LogViewport
            key={`${path}:${formatted}`}
            lines={visibleLines}
            follow={reader.follow}
            onFollow={reader.setFollow}
            accessEntries={accessEntries}
            loginEntries={loginEntries}
            formatted={formatted}
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
      <DialogContent
        className="log-rules-drawer translate-x-0 translate-y-0"
        showCloseButton={false}
      >
        <div className="log-drawer-header">
          <DialogHeader>
            <DialogTitle>显示与隐藏规则</DialogTitle>
            <DialogDescription className="log-drawer-path">
              {path || "请先选择日志文件"}
            </DialogDescription>
          </DialogHeader>
          <DialogClose asChild>
            <Button variant="ghost" size="icon" aria-label="关闭筛选规则">
              <X size={18} />
            </Button>
          </DialogClose>
        </div>
        <div className="log-drawer-body">
          <p className="small-note">
            逐行匹配已加载范围。显示规则命中任意一条即可，隐藏规则优先；没有显示规则时默认显示全部。
          </p>
          <div className="log-drawer-toolbar">
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
              <Plus size={14} />
              添加规则
            </Button>
          </div>
          {rules.length === 0 && (
            <p className="small-note log-rules-empty">
              暂无规则，默认显示全部日志。
            </p>
          )}
          <div className="log-rules">
            {rules.map((rule, index) => (
              <div className="log-rule" key={index}>
                <label className="log-check log-rule-enabled">
                  <Checkbox
                    checked={rule.enabled}
                    onCheckedChange={(checked) =>
                      patchRule(index, { enabled: checked === true })
                    }
                    aria-label={`启用规则 ${index + 1}`}
                  />
                  启用
                </label>
                <label className="log-check log-rule-case">
                  <Checkbox
                    checked={rule.ignore_case}
                    onCheckedChange={(checked) =>
                      patchRule(index, { ignore_case: checked === true })
                    }
                  />
                  忽略大小写
                </label>
                <Button
                  size="icon"
                  variant="ghost"
                  className="log-rule-delete"
                  aria-label={`删除规则 ${index + 1}`}
                  onClick={() => {
                    setRules(rules.filter((_, i) => i !== index));
                    setDirty(true);
                    reader.setRuleError("");
                  }}
                >
                  <Trash2 size={15} />
                </Button>
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
                <Select
                  value={rule.kind}
                  onValueChange={(value) =>
                    patchRule(index, {
                      kind: value as LogRule["kind"],
                    })
                  }
                >
                  <SelectTrigger
                    className="log-rule-kind"
                    aria-label={`规则 ${index + 1} 类型`}
                  >
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent className="log-rule-options">
                    <SelectItem value="keyword">
                      <span className="log-rule-option">
                        <TextSearch size={14} aria-hidden="true" />
                        关键词
                      </span>
                    </SelectItem>
                    <SelectItem value="regex">
                      <span className="log-rule-option">
                        <Regex size={14} aria-hidden="true" />
                        正则
                      </span>
                    </SelectItem>
                  </SelectContent>
                </Select>
                <Select
                  value={rule.action}
                  onValueChange={(value) =>
                    patchRule(index, {
                      action: value as LogRule["action"],
                    })
                  }
                >
                  <SelectTrigger
                    className="log-rule-action"
                    aria-label={`规则 ${index + 1} 操作`}
                  >
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent className="log-rule-options">
                    <SelectItem value="show">
                      <span className="log-rule-option">
                        <Eye size={14} aria-hidden="true" />
                        显示匹配
                      </span>
                    </SelectItem>
                    <SelectItem value="hide">
                      <span className="log-rule-option">
                        <EyeOff size={14} aria-hidden="true" />
                        隐藏匹配
                      </span>
                    </SelectItem>
                  </SelectContent>
                </Select>
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
        </div>
        <div className="log-drawer-footer">
          <DialogClose asChild>
            <Button variant="outline" size="sm">
              关闭
            </Button>
          </DialogClose>
          <Button
            size="sm"
            disabled={!file?.available || reader.busy || !dirty}
            onClick={() => void apply()}
          >
            应用规则
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
