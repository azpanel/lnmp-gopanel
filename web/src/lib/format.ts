export function bytes(value: number | null | undefined, digits = 1): string {
  if (value == null || !Number.isFinite(value)) return "—";
  if (value === 0) return "0 B";
  const i = Math.min(
    4,
    Math.max(0, Math.floor(Math.log(Math.abs(value)) / Math.log(1024))),
  );
  return `${(value / 1024 ** i).toFixed(i === 0 ? 0 : digits)} ${["B", "KiB", "MiB", "GiB", "TiB"][i]}`;
}
export function percent(value: number | null | undefined) {
  return value == null ? "—" : value.toFixed(1);
}
export function uptime(seconds: number | null | undefined) {
  if (seconds == null) return "—";
  const days = Math.floor(seconds / 86400),
    hours = Math.floor((seconds % 86400) / 3600),
    minutes = Math.floor((seconds % 3600) / 60);
  return days > 0
    ? `${days} 天 ${hours} 小时`
    : hours > 0
      ? `${hours} 小时 ${minutes} 分`
      : `${minutes} 分钟`;
}
export function clock(time: number | string) {
  return new Date(time).toLocaleTimeString("zh-CN", { hour12: false });
}
