import {
  useEffect,
  useRef,
  useState,
  type ReactElement,
  type ReactNode,
} from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  CircleHelp,
  FunctionSquare,
  Globe2,
  Hourglass,
  Loader2,
  MemoryStick,
  Plus,
  Save,
  Search,
  Send,
  Settings2,
  Timer,
  Upload,
} from "lucide-react";
import { Tabs, Tooltip } from "radix-ui";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { useSessionMutation } from "@/lib/use-session-mutation";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { Chip, ChipRemove } from "@/components/ui/chip";
import { phpFunctionDescription } from "@/lib/php-functions";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";

type ConfigField = {
  value: string;
  source: string;
  editable: boolean;
  message?: string;
};
type ConfigSnapshot = {
  revision: string;
  path: string;
  editable: boolean;
  message: string;
  fields: Record<string, ConfigField>;
  disable_functions: string[];
  disable_functions_source: string;
  disable_functions_editable: boolean;
  pools: {
    pool: string;
    source: string;
    values: Record<string, string>;
    disable_functions: string[];
  }[];
};
const fields = [
  [
    "upload_max_filesize",
    "单文件上传大小",
    "例如 50M，支持 K/M/G 单位",
    Upload,
    "例如 50M",
  ],
  [
    "post_max_size",
    "POST 请求大小",
    "例如 50M，0 表示不限制",
    Send,
    "例如 50M",
  ],
  [
    "memory_limit",
    "脚本内存限制",
    "例如 128M，-1 表示不限制",
    MemoryStick,
    "例如 128M",
  ],
  [
    "max_execution_time",
    "脚本执行超时",
    "单位：秒，0 表示不限制",
    Timer,
    "例如 300",
  ],
  [
    "max_input_time",
    "输入解析超时",
    "单位：秒，-1 跟随执行超时，0 不限制",
    Hourglass,
    "例如 60",
  ],
  [
    "date.timezone",
    "默认时区",
    "例如 Asia/Shanghai 或 UTC",
    Globe2,
    "例如 Asia/Shanghai",
  ],
  [
    "display_errors",
    "显示错误",
    "生产环境建议关闭，避免向访问者暴露错误信息",
    null,
    "",
  ],
] as const;

function PHPTooltip({
  label,
  children,
  content,
}: {
  label: string;
  children?: ReactElement;
  content: ReactNode;
}) {
  const [open, setOpen] = useState(false);
  return (
    <Tooltip.Root open={open} onOpenChange={setOpen}>
      <Tooltip.Trigger
        asChild
        onClick={(event) => {
          event.preventDefault();
          setOpen(true);
        }}
      >
        {children ?? (
          <button type="button" className="php-config-help" aria-label={label}>
            <CircleHelp size={14} aria-hidden="true" />
          </button>
        )}
      </Tooltip.Trigger>
      <Tooltip.Portal>
        <Tooltip.Content
          className="php-config-tooltip"
          side="top"
          sideOffset={6}
          collisionPadding={16}
        >
          {content}
          <Tooltip.Arrow className="php-config-tooltip-arrow" />
        </Tooltip.Content>
      </Tooltip.Portal>
    </Tooltip.Root>
  );
}

function FunctionChip({
  name,
  onRemove,
  disabled,
}: {
  name: string;
  onRemove?: () => void;
  disabled?: boolean;
}) {
  return (
    <Chip className={onRemove ? undefined : "pr-2"}>
      <PHPTooltip
        label={`${name} 函数说明`}
        content={
          <>
            <strong className="font-mono">{name}</strong>
            {name !== name.toLowerCase() && (
              <p>
                此条目含有大写字母。PHP
                区分禁用函数名的大小写，请使用小写函数名。
              </p>
            )}
            <p>{phpFunctionDescription(name)}</p>
          </>
        }
      >
        <button
          type="button"
          className="php-config-chip-label"
          aria-label={`${name} 函数说明`}
        >
          {name}
        </button>
      </PHPTooltip>
      {onRemove && (
        <ChipRemove
          aria-label={`删除 ${name}`}
          disabled={disabled}
          onClick={onRemove}
        />
      )}
    </Chip>
  );
}
const endpoint = "/services/php-fpm/config";
const queryKey = ["php-config"];

export function PHPConfig({ disabled }: { disabled?: boolean }) {
  const [open, setOpen] = useState(false);
  const [dirty, setDirty] = useState(false);
  const [saving, setSaving] = useState(false);
  const [discard, setDiscard] = useState(false);
  const trigger = useRef<HTMLButtonElement>(null);
  const closeTarget = useRef<HTMLElement | null>(null);
  const close = () => {
    if (saving) return;
    if (dirty) {
      closeTarget.current =
        document.activeElement instanceof HTMLElement
          ? document.activeElement
          : null;
      setDiscard(true);
    } else setOpen(false);
  };
  return (
    <Tooltip.Provider delayDuration={150}>
      <Dialog
        open={open}
        onOpenChange={(next) => {
          if (next) {
            setDirty(false);
            setDiscard(false);
            setOpen(true);
          } else close();
        }}
      >
        <Button
          ref={trigger}
          variant="outline"
          size="sm"
          disabled={disabled}
          onClick={() => {
            setDirty(false);
            setDiscard(false);
            setOpen(true);
          }}
        >
          <Settings2 size={13} />
          配置管理
        </Button>
        <DialogContent
          className="php-config-dialog max-h-[90dvh] overflow-y-auto sm:max-w-3xl"
          showCloseButton={!saving}
          onCloseAutoFocus={(event) => {
            event.preventDefault();
            trigger.current?.focus();
          }}
        >
          <DialogHeader>
            <DialogTitle>PHP-FPM 配置管理</DialogTitle>
            <DialogDescription>
              保存时检查配置并自动重载 PHP-FPM。
            </DialogDescription>
          </DialogHeader>
          {open && (
            <PHPConfigEditor
              onDirty={setDirty}
              onSaving={setSaving}
              onClose={close}
            />
          )}
          <Dialog open={discard} onOpenChange={setDiscard}>
            <DialogContent
              role="alertdialog"
              className="sm:max-w-sm"
              showCloseButton={false}
              onCloseAutoFocus={(event) => {
                event.preventDefault();
                if (open) closeTarget.current?.focus();
                else trigger.current?.focus();
              }}
            >
              <DialogHeader>
                <DialogTitle className="text-base">
                  放弃未保存的修改？
                </DialogTitle>
                <DialogDescription>
                  有未保存的修改，确定放弃并关闭？
                </DialogDescription>
              </DialogHeader>
              <DialogFooter>
                <Button variant="outline" onClick={() => setDiscard(false)}>
                  继续编辑
                </Button>
                <Button
                  variant="destructive"
                  onClick={() => {
                    setDiscard(false);
                    setDirty(false);
                    setOpen(false);
                  }}
                >
                  放弃修改
                </Button>
              </DialogFooter>
            </DialogContent>
          </Dialog>
        </DialogContent>
      </Dialog>
    </Tooltip.Provider>
  );
}

function PHPConfigEditor({
  onDirty,
  onSaving,
  onClose,
}: {
  onDirty: (dirty: boolean) => void;
  onSaving: (saving: boolean) => void;
  onClose: () => void;
}) {
  const client = useQueryClient();
  const [tab, setTab] = useState<"common" | "functions">("common");
  const [draft, setDraft] = useState<Record<string, string> | null>(null);
  const [functions, setFunctions] = useState<string[]>([]);
  const [addition, setAddition] = useState("");
  const [search, setSearch] = useState("");
  const [localError, setLocalError] = useState("");
  const [refreshConfirm, setRefreshConfirm] = useState(false);
  const query = useQuery({
    queryKey,
    queryFn: ({ signal }) => api<ConfigSnapshot>(endpoint, { signal }),
    gcTime: 0,
    retry: false,
    refetchOnWindowFocus: false,
  });
  const snapshot = query.data;
  useEffect(() => {
    if (!snapshot) return;
    setDraft(
      Object.fromEntries(
        Object.entries(snapshot.fields).map(([key, item]) => [key, item.value]),
      ),
    );
    setFunctions(snapshot.disable_functions);
    setAddition("");
  }, [snapshot]);
  const changes = Object.fromEntries(
    Object.entries(draft ?? {}).filter(
      ([key, value]) => value !== snapshot?.fields[key]?.value,
    ),
  );
  const functionsChanged =
    JSON.stringify([...functions].sort()) !==
    JSON.stringify([...(snapshot?.disable_functions ?? [])].sort());
  const changed = Object.keys(changes).length > 0 || functionsChanged;
  useEffect(
    () => onDirty(changed || addition.trim() !== ""),
    [changed, addition, onDirty],
  );
  const save = useSessionMutation({
    mutationFn: () =>
      api<ConfigSnapshot>(endpoint, {
        method: "PUT",
        body: JSON.stringify({
          revision: snapshot?.revision,
          fields: changes,
          ...(functionsChanged ? { disable_functions: functions } : {}),
        }),
      }),
    onSuccess: (result) => {
      client.setQueryData(queryKey, result);
      setLocalError("");
      toast.success(result.message);
    },
    onSettled: () => client.invalidateQueries({ queryKey: ["services"] }),
  });
  useEffect(() => onSaving(save.isPending), [save.isPending, onSaving]);
  const editable = Boolean(
    snapshot?.editable && !query.isFetching && !query.error && !save.isPending,
  );
  const functionsEditable = editable && snapshot?.disable_functions_editable;
  const addFunctions = () => {
    const names = addition.split(",").map((name) => name.trim().toLowerCase());
    if (
      names.some(
        (name) => !/^[a-z_][a-z0-9_]*$/.test(name) || name.length > 128,
      )
    ) {
      setLocalError("请输入有效的函数名，多个函数用半角逗号分隔。");
      return;
    }
    const added = new Set(names);
    const next = [
      ...functions.filter((name) => !added.has(name.toLowerCase())),
      ...added,
    ].sort();
    if (next.length > 512) {
      setLocalError("禁用函数数量不能超过 512。");
      return;
    }
    setFunctions(next);
    setAddition("");
    setLocalError("");
  };
  const refresh = () => {
    setRefreshConfirm(false);
    setLocalError("");
    save.reset();
    void query.refetch().then((result) => {
      if (!result.isError && result.data) {
        setDraft(
          Object.fromEntries(
            Object.entries(result.data.fields).map(([key, item]) => [
              key,
              item.value,
            ]),
          ),
        );
        setFunctions(result.data.disable_functions);
        setAddition("");
      }
    });
  };
  const size = (value: string) => {
    const match = /^(\d+)([kmg]?)$/i.exec(value);
    return match
      ? Number(match[1]) *
          1024 ** (match[2] ? "kmg".indexOf(match[2].toLowerCase()) + 1 : 0)
      : NaN;
  };
  return (
    <>
      {query.isPending && <p role="status">正在读取 PHP 配置…</p>}
      {query.error && (
        <p role="alert" className="text-sm text-destructive">
          配置读取失败：{query.error.message}
        </p>
      )}
      {snapshot && draft && (
        <>
          {!snapshot.editable && (
            <p className="php-config-status" role="status">
              {snapshot.message}
            </p>
          )}
          <Tabs.Root
            value={tab}
            onValueChange={(value) => setTab(value as typeof tab)}
            className="php-config-tabs"
          >
            <div className="php-config-tab-heading">
              <Tabs.List
                aria-label="PHP 配置分类"
                className="policy-tabs-list php-config-tabs-list"
              >
                <Tabs.Trigger value="common">常用配置</Tabs.Trigger>
                <Tabs.Trigger value="functions">
                  禁用函数{" "}
                  <span className="php-config-count">{functions.length}</span>
                </Tabs.Trigger>
              </Tabs.List>
              <PHPTooltip
                label="全局配置说明"
                content={
                  <>
                    <p>修改全局 PHP 配置，网站和进程池可覆盖部分参数。</p>
                    <p>配置文件：{snapshot.path}</p>
                  </>
                }
              />
            </div>
            <Tabs.Content value="common" className="php-config-common">
              {fields.map(([key, label, hint, Icon, placeholder]) => {
                const field = snapshot.fields[key];
                const help = `${key}。${hint}。来源：${field.source}${field.message ? `。${field.message}` : ""}`;
                return (
                  <div key={key} className="php-config-field">
                    <div className="php-config-field-heading">
                      <label htmlFor={`php-${key}`}>
                        {Icon && <Icon size={14} aria-hidden="true" />}
                        {label}
                      </label>
                      {!field.editable && (
                        <span className="php-config-readonly">只读</span>
                      )}
                      <PHPTooltip
                        label={`${label}说明`}
                        content={
                          <>
                            <p className="font-mono">{key}</p>
                            <p>{hint}</p>
                            <p>来源：{field.source}</p>
                            {field.message && <p>{field.message}</p>}
                          </>
                        }
                      />
                    </div>
                    <span id={`php-${key}-help`} className="sr-only">
                      {help}
                    </span>
                    {key === "display_errors" ? (
                      <div className="php-config-switch">
                        <Switch
                          id={`php-${key}`}
                          aria-describedby={`php-${key}-help`}
                          checked={draft[key] === "On"}
                          disabled={!editable || !field.editable}
                          onCheckedChange={(checked) =>
                            setDraft({
                              ...draft,
                              [key]: checked ? "On" : "Off",
                            })
                          }
                        />
                        <span>{draft[key] === "On" ? "开启" : "关闭"}</span>
                      </div>
                    ) : (
                      <Input
                        id={`php-${key}`}
                        aria-describedby={`php-${key}-help`}
                        value={draft[key]}
                        disabled={!editable || !field.editable}
                        placeholder={placeholder}
                        onChange={(event) =>
                          setDraft({ ...draft, [key]: event.target.value })
                        }
                      />
                    )}
                  </div>
                );
              })}
              {size(draft.post_max_size) > 0 &&
                size(draft.post_max_size) < size(draft.upload_max_filesize) && (
                  <p className="php-config-size-warning" role="status">
                    POST 请求大小小于单文件上传大小，实际上传会受 POST
                    请求大小限制。
                  </p>
                )}
            </Tabs.Content>
            <Tabs.Content value="functions" className="php-config-functions">
              <div className="php-config-field-heading">
                <label htmlFor="php-config-add">
                  <FunctionSquare size={14} aria-hidden="true" />
                  添加禁用函数
                </label>
                {!snapshot.disable_functions_editable && (
                  <span className="php-config-readonly">只读</span>
                )}
                <PHPTooltip
                  label="禁用函数配置说明"
                  content={
                    <>
                      <p className="font-mono">disable_functions</p>
                      <p>多个函数用半角逗号分隔，重复项会自动合并。</p>
                      <p>来源：{snapshot.disable_functions_source}</p>
                      {!snapshot.disable_functions_editable && (
                        <p>被扫描 INI 覆盖，请在来源文件中修改。</p>
                      )}
                      <p>进程池额外禁用的同名函数不会随全局移除而解除。</p>
                    </>
                  }
                />
              </div>
              <form
                className="php-config-add"
                onSubmit={(event) => {
                  event.preventDefault();
                  if (functionsEditable && addition.trim()) addFunctions();
                }}
              >
                <Input
                  id="php-config-add"
                  placeholder="例如 exec,shell_exec"
                  value={addition}
                  disabled={!functionsEditable}
                  onChange={(event) => setAddition(event.target.value)}
                />
                <Button
                  type="submit"
                  variant="outline"
                  size="sm"
                  disabled={!functionsEditable || !addition.trim()}
                >
                  <Plus size={14} />
                  添加
                </Button>
              </form>
              <div className="php-config-search">
                <Search size={14} aria-hidden="true" />
                <Input
                  aria-label="搜索禁用函数"
                  placeholder="搜索函数名"
                  value={search}
                  onChange={(event) => setSearch(event.target.value)}
                />
              </div>
              <ul aria-label="全局禁用函数" className="php-config-chips">
                {functions
                  .filter((name) =>
                    name.toLowerCase().includes(search.toLowerCase().trim()),
                  )
                  .map((name) => (
                    <li key={name}>
                      <FunctionChip
                        name={name}
                        disabled={!functionsEditable}
                        onRemove={() =>
                          setFunctions(
                            functions.filter((item) => item !== name),
                          )
                        }
                      />
                    </li>
                  ))}
              </ul>
              {functions.length === 0 ? (
                <p className="small-note">未禁用任何函数。</p>
              ) : (
                !functions.some((name) =>
                  name.toLowerCase().includes(search.toLowerCase().trim()),
                ) && <p className="small-note">没有匹配的函数。</p>
              )}
              <p className="small-note">
                增删将在保存后生效；进程池可能继续禁用同名函数。
              </p>
            </Tabs.Content>
          </Tabs.Root>
          {snapshot.pools.some(
            (pool) => Object.keys(pool.values).length > 0,
          ) && (
            <details className="rounded-md border p-3 text-sm">
              <summary className="cursor-pointer font-medium">
                进程池额外配置（只读）
              </summary>
              <p className="small-note mt-2">
                进程池可覆盖常用参数；禁用函数会追加到全局列表。
              </p>
              {snapshot.pools
                .filter((pool) => Object.keys(pool.values).length > 0)
                .map((pool) => (
                  <div key={pool.pool} className="mt-3 min-w-0 space-y-1">
                    <div className="php-config-field-heading">
                      <span className="font-medium">{pool.pool}</span>
                      <PHPTooltip
                        label={`${pool.pool} 进程池配置来源`}
                        content={<p>来源：{pool.source}</p>}
                      />
                    </div>
                    {Object.entries(pool.values).map(([key, value]) =>
                      key === "disable_functions" ? (
                        <div key={key} className="space-y-1">
                          <p className="font-mono text-xs">disable_functions</p>
                          {pool.disable_functions.length ? (
                            <ul
                              aria-label={`${pool.pool} 进程池禁用函数`}
                              className="php-config-chips"
                            >
                              {pool.disable_functions.map((name) => (
                                <li key={name}>
                                  <FunctionChip name={name} />
                                </li>
                              ))}
                            </ul>
                          ) : (
                            <p className="small-note">空</p>
                          )}
                        </div>
                      ) : (
                        <p key={key} className="break-all font-mono text-xs">
                          {key} = {value}
                        </p>
                      ),
                    )}
                  </div>
                ))}
            </details>
          )}
        </>
      )}
      {(localError || save.error) && (
        <p role="alert" className="text-sm text-destructive">
          {localError || save.error?.message}
        </p>
      )}
      {refreshConfirm ? (
        <div className="space-y-3 rounded-md border p-3" role="alert">
          <p className="text-sm">重新读取会放弃当前草稿，确定继续？</p>
          <div className="flex justify-end gap-2">
            <Button variant="outline" onClick={() => setRefreshConfirm(false)}>
              继续编辑
            </Button>
            <Button onClick={refresh}>放弃并读取</Button>
          </div>
        </div>
      ) : (
        <DialogFooter>
          <Button variant="outline" disabled={save.isPending} onClick={onClose}>
            关闭
          </Button>
          <Button
            variant="outline"
            disabled={save.isPending || query.isFetching}
            onClick={() => {
              if (changed || addition.trim()) setRefreshConfirm(true);
              else refresh();
            }}
          >
            重新读取
          </Button>
          <Button
            disabled={!editable || !changed || Boolean(addition.trim())}
            onClick={() => {
              setLocalError("");
              save.mutate();
            }}
          >
            {save.isPending ? (
              <Loader2 size={14} className="animate-spin" />
            ) : (
              <Save size={14} />
            )}
            {save.isPending ? "正在保存并重载…" : "保存并重载"}
          </Button>
        </DialogFooter>
      )}
    </>
  );
}
