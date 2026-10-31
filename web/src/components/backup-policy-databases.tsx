import { useEffect, useId, useRef, useState } from "react";
import {
  Fingerprint,
  HardDriveDownload,
  Layers3,
  ListFilter,
  ScanSearch,
  Search,
} from "lucide-react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Checkbox } from "@/components/ui/checkbox";
import { Switch } from "@/components/ui/switch";
import { PolicyLabel, PolicySelect } from "@/components/backup-policy-fields";
import { api } from "@/lib/api";
import {
  databaseSelected,
  toggleDatabase,
  readableBytes,
  type DatabaseItem,
  type DatabaseSelection,
  type TableList,
  type TableRule,
} from "@/lib/backup-policies";

export function BackupPolicyDatabases({
  value,
  onChange,
  readOnly,
  active,
}: {
  value: DatabaseSelection;
  onChange: (v: DatabaseSelection) => void;
  readOnly: boolean;
  active: boolean;
}) {
  const instance = useId();
  const password = useRef("");
  const [epoch, setEpoch] = useState(0);
  const [search, setSearch] = useState("");
  const [current, setCurrent] = useState("");
  const [authOpen, setAuthOpen] = useState(false);
  const detail = useRef<HTMLDivElement>(null);
  const client = useQueryClient();
  useEffect(
    () => () => {
      password.current = "";
      client.removeQueries({ queryKey: ["backup-database-inspect", instance] });
    },
    [client, instance],
  );
  const query = useQuery({
    queryKey: ["backup-database-inspect", instance, epoch, "databases"],
    enabled: active && value.enabled,
    queryFn: ({ signal }) =>
      api<DatabaseItem[]>("/backups/databases/inspect", {
        method: "POST",
        body: JSON.stringify({ password: password.current }),
        signal,
      }),
    retry: false,
    refetchOnWindowFocus: false,
  });
  useEffect(() => {
    if (query.isError) setAuthOpen(true);
    else if (query.isSuccess) setAuthOpen(false);
  }, [query.isError, query.isSuccess]);
  const actual = query.data ?? [];
  const allNames = [
    ...new Set([
      ...actual.map((item) => item.name),
      ...value.selected,
      ...value.rules.map((r) => r.database),
    ]),
  ];
  const names = allNames.filter((name) =>
    name.toLowerCase().includes(search.toLowerCase()),
  );
  const name = names.includes(current)
    ? current
    : (names.find((n) => databaseSelected(value, n)) ?? names[0] ?? "");
  const rule = value.rules.find((r) => r.database === name) ?? {
    database: name,
    mode: "all" as const,
    tables: [],
  };
  const selected = databaseSelected(value, name);
  const updateRule = (next: TableRule) =>
    onChange({
      ...value,
      rules: [...value.rules.filter((r) => r.database !== name), next],
    });
  function view(database: string) {
    setCurrent(database);
    if (window.matchMedia("(max-width: 720px)").matches)
      requestAnimationFrame(() =>
        detail.current?.scrollIntoView({ block: "nearest" }),
      );
  }
  return (
    <div className="policy-section policy-database-pane">
      <div className="policy-db-controls">
        <label className="policy-switch">
          <Switch
            aria-label="备份数据库"
            checked={value.enabled}
            disabled={readOnly}
            onCheckedChange={(enabled) => onChange({ ...value, enabled })}
          />
          <PolicyLabel icon={HardDriveDownload}>备份数据库</PolicyLabel>
        </label>
        {value.enabled && (
          <PolicySelect
            label="数据库范围"
            icon={Layers3}
            value={value.mode}
            disabled={readOnly}
            options={[
              ["all", "全部数据库（自动包含日后新增库）"],
              ["selected", "指定数据库"],
            ]}
            onChange={(mode) =>
              onChange({
                ...value,
                mode: mode as "all" | "selected",
                selected:
                  mode === "selected"
                    ? actual
                        .filter((db) => databaseSelected(value, db.name))
                        .map((db) => db.name)
                    : value.selected,
              })
            }
          />
        )}
      </div>
      {value.enabled && (
        <>
          <details
            className="policy-db-connection"
            open={authOpen}
            onToggle={(e) => setAuthOpen(e.currentTarget.open)}
          >
            <summary>
              <PolicyLabel icon={Fingerprint}>数据库认证</PolicyLabel>
              <span className="small-note">
                {query.isError
                  ? "连接失败，请输入本机 root 密码"
                  : "按需填写，仅用于本次查询"}
              </span>
            </summary>
            <div className="policy-database-auth">
              <label>
                管理员密码（仅本次查询）
                <Input
                  type="password"
                  aria-label="数据库管理员密码"
                  autoComplete="off"
                  onChange={(e) => {
                    password.current = e.target.value;
                  }}
                />
              </label>
              <Button
                type="button"
                variant="outline"
                disabled={query.isFetching}
                onClick={() => setEpoch((n) => n + 1)}
              >
                重新连接
              </Button>
            </div>
          </details>
          {query.error && (
            <div className="notice" role="alert">
              {query.error.message}。已选规则保留。
            </div>
          )}
          <div className="policy-database-workspace">
            <div className="policy-database-sidebar">
              <div className="policy-db-list-toolbar">
                <strong>数据库</strong>
                <span className="small-note">
                  已选{" "}
                  {allNames.filter((n) => databaseSelected(value, n)).length} /{" "}
                  {allNames.length}
                </span>
                <Button
                  type="button"
                  size="sm"
                  variant="ghost"
                  disabled={query.isFetching}
                  onClick={() => setEpoch((n) => n + 1)}
                >
                  刷新数据库
                </Button>
              </div>
              <div className="policy-search-field">
                <Search size={16} aria-hidden="true" />
                <Input
                  aria-label="搜索数据库"
                  placeholder="搜索数据库"
                  value={search}
                  onChange={(e) => setSearch(e.target.value)}
                />
              </div>
              <div className="policy-database-list">
                {names.map((database) => {
                  const item = actual.find((db) => db.name === database);
                  return (
                    <div
                      className="policy-database policy-database-row"
                      data-active={database === name}
                      key={database}
                    >
                      <Checkbox
                        aria-label={`备份数据库 ${database}`}
                        checked={databaseSelected(value, database)}
                        disabled={readOnly}
                        onCheckedChange={(checked) =>
                          onChange(
                            toggleDatabase(value, database, checked === true),
                          )
                        }
                      />
                      <button
                        type="button"
                        className="policy-db-name"
                        title={database}
                        onClick={() => view(database)}
                      >
                        {database}
                      </button>
                      <span className="policy-db-size">
                        {item ? readableBytes(item.size) : "未读取到"}
                      </span>
                      <Button
                        type="button"
                        size="sm"
                        variant="ghost"
                        aria-label={`查看 ${database} 的表`}
                        aria-pressed={database === name}
                        onClick={() => view(database)}
                      >
                        查看表
                      </Button>
                    </div>
                  );
                })}
                {query.isFetching && <p role="status">正在读取数据库…</p>}
                {!names.length && !query.isFetching && (
                  <p className="small-note">没有匹配的数据库。</p>
                )}
              </div>
              <p className="small-note policy-db-exclusion-note">
                默认跳过
                mysql、information_schema、performance_schema、phpmyadmin、sys，可手动勾选。
              </p>
            </div>
            <div
              className="policy-database-detail"
              ref={detail}
              aria-label="数据库表设置"
            >
              {name ? (
                <>
                  <div className="policy-db-detail-heading">
                    <strong>{name}</strong>
                    <span className="small-note">
                      大小为数据与索引的元数据估算
                    </span>
                  </div>
                  <PolicySelect
                    label={`${name} 的表规则`}
                    icon={ListFilter}
                    value={rule.mode}
                    disabled={readOnly || !selected}
                    options={[
                      ["all", "全部表（包含新增表）"],
                      ["include", "仅备份所选表"],
                      ["exclude", "跳过所选表（包含其他新增表）"],
                    ]}
                    onChange={(mode) =>
                      updateRule({ ...rule, mode: mode as TableRule["mode"] })
                    }
                  />
                  {!selected && (
                    <p className="small-note">
                      该数据库未勾选，其表规则暂不生效。
                    </p>
                  )}
                  <DatabaseTables
                    key={name}
                    database={name}
                    instance={instance}
                    epoch={epoch}
                    password={password}
                    active={active}
                    readOnly={readOnly || !selected}
                    rule={rule}
                    onChange={updateRule}
                  />
                </>
              ) : (
                <p className="small-note">选择左侧数据库，查看表及备份规则。</p>
              )}
            </div>
          </div>
        </>
      )}
    </div>
  );
}
function DatabaseTables({
  database,
  instance,
  epoch,
  password,
  active,
  readOnly,
  rule,
  onChange,
}: {
  database: string;
  instance: string;
  epoch: number;
  password: { current: string };
  active: boolean;
  readOnly: boolean;
  rule: TableRule;
  onChange: (v: TableRule) => void;
}) {
  const [search, setSearch] = useState("");
  const [page, setPage] = useState(1);
  const [debounced, setDebounced] = useState("");
  useEffect(() => {
    const timer = setTimeout(() => {
      setDebounced(search);
      setPage(1);
    }, 300);
    return () => clearTimeout(timer);
  }, [search]);
  const query = useQuery({
    queryKey: [
      "backup-database-inspect",
      instance,
      epoch,
      "tables",
      database,
      debounced,
      page,
    ],
    enabled: active,
    queryFn: ({ signal }) =>
      api<TableList>("/backups/databases/tables", {
        method: "POST",
        body: JSON.stringify({
          password: password.current,
          database,
          search: debounced,
          page,
        }),
        signal,
      }),
    retry: false,
    refetchOnWindowFocus: false,
  });
  return (
    <div className="policy-section policy-table-browser">
      <div className="policy-search-field">
        <ScanSearch size={16} aria-hidden="true" />
        <Input
          aria-label={`搜索 ${database} 的表`}
          placeholder="搜索表"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
      </div>
      {query.isFetching && <p role="status">正在读取表…</p>}
      {query.error && (
        <p role="alert">
          {query.error.message}
          <Button
            type="button"
            size="sm"
            variant="outline"
            onClick={() => void query.refetch()}
          >
            重试
          </Button>
        </p>
      )}
      <div className="policy-table-list">
        {query.data?.items.map((table) => (
          <div className="policy-table-row" key={table.name}>
            <label className="policy-check">
              <Checkbox
                aria-label={`${database}.${table.name}`}
                checked={
                  rule.mode === "all" || rule.tables.includes(table.name)
                }
                disabled={readOnly || rule.mode === "all"}
                onCheckedChange={(checked) =>
                  onChange({
                    ...rule,
                    tables:
                      checked === true
                        ? [...new Set([...rule.tables, table.name])]
                        : rule.tables.filter((t) => t !== table.name),
                  })
                }
              />
              <span>
                {table.name}
                {table.type === "VIEW" ? "（视图）" : ""}
              </span>
            </label>
            <span>{readableBytes(table.size)}</span>
          </div>
        ))}
      </div>
      {query.data && (
        <div className="policy-inline">
          <Button
            type="button"
            size="sm"
            variant="outline"
            disabled={page === 1}
            onClick={() => setPage((n) => n - 1)}
          >
            上一页
          </Button>
          <span>
            第 {page} 页，共 {query.data.total} 项
          </span>
          <Button
            type="button"
            size="sm"
            variant="outline"
            disabled={page * query.data.page_size >= query.data.total}
            onClick={() => setPage((n) => n + 1)}
          >
            下一页
          </Button>
        </div>
      )}
      {rule.mode !== "all" && (
        <div>
          <p className="small-note">
            {rule.mode === "include" ? "仅备份" : "跳过"}以下{" "}
            {rule.tables.length} 张表（跨页保留）：
          </p>
          <div className="policy-selected-tables">
            {rule.tables.map((name) => (
              <Button
                key={name}
                type="button"
                variant="outline"
                size="sm"
                disabled={readOnly}
                onClick={() =>
                  onChange({
                    ...rule,
                    tables: rule.tables.filter((n) => n !== name),
                  })
                }
              >
                {name} ×
              </Button>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}
