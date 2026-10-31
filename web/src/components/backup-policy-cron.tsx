import { useEffect, useId, useState } from "react";
import {
  Braces,
  CalendarCheck2,
  CalendarClock,
  CalendarDays,
  CalendarRange,
  Clock3,
  Hourglass,
  PencilRuler,
  Repeat2,
  Timer,
  type LucideIcon,
} from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { api } from "@/lib/api";
import { PolicyLabel, PolicySelect } from "@/components/backup-policy-fields";

type Preview = { timezone: string; times: string[]; message?: string };
const presets: Record<string, string> = {
  daily: "0 2 * * *",
  weekly: "0 2 * * 0",
  monthly: "0 2 1 * *",
  hourly: "0 * * * *",
  minutes: "*/30 * * * *",
};
const fieldNames = ["分钟", "小时", "日期", "月份", "星期"];
const fieldIcons: LucideIcon[] = [
  Timer,
  Clock3,
  CalendarDays,
  CalendarRange,
  CalendarCheck2,
];
const limits = [
  [0, 59],
  [0, 23],
  [1, 31],
  [1, 12],
  [0, 6],
];
function frequency(value: string) {
  const f = value.trim().split(/\s+/);
  const number = (v: string) => /^\d+$/.test(v);
  if (f.length !== 5) return "custom";
  if (number(f[0]) && number(f[1]) && f[2] === "*" && f[3] === "*")
    return f[4] === "*" ? "daily" : number(f[4]) ? "weekly" : "custom";
  if (
    number(f[0]) &&
    number(f[1]) &&
    number(f[2]) &&
    f[3] === "*" &&
    f[4] === "*"
  )
    return "monthly";
  if (number(f[0]) && f.slice(1).every((v) => v === "*")) return "hourly";
  if (/^\*\/\d+$/.test(f[0]) && f.slice(1).every((v) => v === "*"))
    return "minutes";
  return "custom";
}
export function BackupPolicyCron({
  value,
  onChange,
  readOnly,
}: {
  value: string;
  onChange: (v: string) => void;
  readOnly: boolean;
}) {
  const [mode, setMode] = useState(() => frequency(value));
  const [advanced, setAdvanced] = useState(() => frequency(value) === "custom");
  const [method, setMethod] = useState("expression");
  const [showTimes, setShowTimes] = useState(false);
  const [expression, setExpression] = useState(value);
  const advancedId = useId();
  const timesId = useId();
  useEffect(() => {
    const timer = setTimeout(() => setExpression(value), 350);
    return () => clearTimeout(timer);
  }, [value]);
  const preview = useQuery({
    queryKey: ["backup-schedule-preview", expression],
    queryFn: ({ signal }) =>
      api<Preview>("/backups/schedule/preview", {
        method: "POST",
        body: JSON.stringify({ schedule: expression }),
        signal,
      }),
    retry: false,
  });
  const fields = value.trim().split(/\s+/);
  const update = (index: number, field: string, custom = false) => {
    const next = fields.length === 5 ? [...fields] : ["*", "*", "*", "*", "*"];
    next[index] = field;
    if (custom) setMode("custom");
    onChange(next.join(" "));
  };
  return (
    <div className="policy-section policy-frequency">
      <div className="policy-frequency-row" data-mode={mode}>
        <PolicySelect
          label="频率编辑方式"
          icon={Repeat2}
          value={mode}
          disabled={readOnly}
          options={[
            ["daily", "每天"],
            ["weekly", "每周"],
            ["monthly", "每月"],
            ["hourly", "每小时"],
            ["minutes", "每隔 N 分钟"],
            ["custom", "自定义 Cron"],
          ]}
          onChange={(v) => {
            setMode(v);
            if (presets[v]) onChange(presets[v]);
            if (v === "custom") setAdvanced(true);
          }}
        />
        {["daily", "weekly", "monthly"].includes(mode) && (
          <>
            <label>
              <PolicyLabel icon={Clock3}>小时</PolicyLabel>
              <Input
                aria-label="计划小时"
                type="number"
                min={0}
                max={23}
                value={fields[1] || ""}
                disabled={readOnly}
                onChange={(e) => update(1, e.target.value)}
              />
            </label>
            <label>
              <PolicyLabel icon={Timer}>分钟</PolicyLabel>
              <Input
                aria-label="计划分钟"
                type="number"
                min={0}
                max={59}
                value={fields[0] || ""}
                disabled={readOnly}
                onChange={(e) => update(0, e.target.value)}
              />
            </label>
          </>
        )}
        {mode === "weekly" && (
          <PolicySelect
            label="星期"
            icon={CalendarCheck2}
            value={fields[4] || "0"}
            disabled={readOnly}
            onChange={(v) => update(4, v)}
            options={[
              ["0", "周日"],
              ["1", "周一"],
              ["2", "周二"],
              ["3", "周三"],
              ["4", "周四"],
              ["5", "周五"],
              ["6", "周六"],
            ]}
          />
        )}
        {mode === "monthly" && (
          <label>
            <PolicyLabel icon={CalendarDays}>每月日期</PolicyLabel>
            <Input
              aria-label="每月日期"
              type="number"
              min={1}
              max={31}
              value={fields[2] || ""}
              disabled={readOnly}
              onChange={(e) => update(2, e.target.value)}
            />
          </label>
        )}
        {mode === "hourly" && (
          <label>
            <PolicyLabel icon={Timer}>分钟</PolicyLabel>
            <Input
              aria-label="计划分钟"
              type="number"
              min={0}
              max={59}
              value={fields[0] || ""}
              disabled={readOnly}
              onChange={(e) => update(0, e.target.value)}
            />
          </label>
        )}
        {mode === "minutes" && (
          <label>
            <PolicyLabel icon={Hourglass}>间隔分钟数</PolicyLabel>
            <Input
              aria-label="间隔分钟数"
              type="number"
              min={1}
              max={59}
              value={fields[0]?.replace("*/", "") || ""}
              disabled={readOnly}
              onChange={(e) => update(0, `*/${e.target.value}`)}
            />
          </label>
        )}
      </div>
      <div className="policy-cron-summary">
        <PolicyLabel icon={Braces}>Cron</PolicyLabel>
        <code>{value}</code>
        <Button
          type="button"
          size="sm"
          variant="ghost"
          aria-expanded={advanced}
          aria-controls={advancedId}
          onClick={() => setAdvanced((v) => !v)}
        >
          {advanced ? "收起高级编辑" : "高级编辑"}
        </Button>
      </div>
      {advanced && (
        <div id={advancedId} className="policy-cron-advanced">
          <PolicySelect
            label="高级编辑方式"
            icon={PencilRuler}
            value={method}
            disabled={readOnly}
            options={[
              ["expression", "直接输入表达式"],
              ["fields", "五字段高级表单"],
            ]}
            onChange={setMethod}
          />
          {method === "fields" && (
            <div className="policy-cron-fields">
              {fieldNames.map((name, i) => (
                <CronField
                  key={name}
                  name={name}
                  icon={fieldIcons[i]}
                  value={fields[i] || "*"}
                  min={limits[i][0]}
                  max={limits[i][1]}
                  readOnly={readOnly}
                  onChange={(v) => update(i, v, true)}
                />
              ))}
            </div>
          )}
          <label htmlFor="policy-cron">
            Cron 表达式
            <Input
              id="policy-cron"
              className="font-mono"
              value={value}
              readOnly={readOnly || method !== "expression"}
              onChange={(e) => {
                setMode("custom");
                onChange(e.target.value);
              }}
            />
          </label>
          <p className="small-note">
            五字段：分钟 小时 日期 月份
            星期；日期与星期均有限定时按“或”规则匹配。
          </p>
        </div>
      )}
      <div className="policy-next-run" aria-live="polite">
        <PolicyLabel icon={CalendarClock}>下一次计划</PolicyLabel>
        {preview.isFetching ? (
          <span role="status">计算中…</span>
        ) : preview.error ? (
          <span className="form-error" role="alert">
            {preview.error.message}
          </span>
        ) : (
          <time dateTime={preview.data?.times[0]}>
            {preview.data?.times[0]?.slice(0, 19).replace("T", " ") || "—"}
          </time>
        )}
        <Button
          type="button"
          size="sm"
          variant="ghost"
          aria-expanded={showTimes}
          aria-controls={timesId}
          onClick={() => setShowTimes((v) => !v)}
        >
          {showTimes ? "收起计划时间" : "查看五次"}
        </Button>
      </div>
      {showTimes && (
        <div id={timesId} className="policy-schedule-preview">
          <p className="small-note">{preview.data?.timezone}</p>
          <ol>
            {preview.data?.times.map((time) => (
              <li key={time}>{time.replace("T", " ")}</li>
            ))}
          </ol>
          {preview.data?.message && <p>{preview.data.message}</p>}
        </div>
      )}
    </div>
  );
}
function CronField({
  name,
  icon,
  value,
  min,
  max,
  readOnly,
  onChange,
}: {
  name: string;
  icon: LucideIcon;
  value: string;
  min: number;
  max: number;
  readOnly: boolean;
  onChange: (v: string) => void;
}) {
  const type =
    value === "*"
      ? "any"
      : value.startsWith("*/")
        ? "step"
        : value.includes("-")
          ? "range"
          : "values";
  return (
    <div>
      <PolicySelect
        label={name}
        icon={icon}
        value={type}
        disabled={readOnly}
        options={[
          ["any", "任意"],
          ["values", "指定值/列表"],
          ["range", "范围"],
          ["step", "间隔"],
        ]}
        onChange={(v) =>
          onChange(
            v === "any"
              ? "*"
              : v === "step"
                ? "*/2"
                : v === "range"
                  ? `${min}-${max}`
                  : `${min}`,
          )
        }
      />
      {type !== "any" && (
        <Input
          aria-label={`${name}字段值`}
          value={value}
          disabled={readOnly}
          onChange={(e) => onChange(e.target.value)}
        />
      )}
      <span className="small-note">
        {min}–{max}
      </span>
    </div>
  );
}
