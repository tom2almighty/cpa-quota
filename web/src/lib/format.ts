/** 将 0~1 的剩余比例格式化为整数百分比 */
export function formatPercent(fraction: number): string {
  if (!Number.isFinite(fraction)) return "—";
  return `${Math.round(Math.max(0, Math.min(1, fraction)) * 100)}%`;
}

/** ISO 时间 → 本地 "MM-DD HH:mm" */
export function formatDateTime(value: string | undefined): string {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "—";
  return new Intl.DateTimeFormat(undefined, {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  }).format(date);
}

/** 相对时间，用于 "x 分钟前更新" */
export function formatRelative(timestamp: number): string {
  if (!timestamp) return "—";
  const seconds = Math.max(0, Math.round((Date.now() - timestamp) / 1000));
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.round(seconds / 60);
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.round(minutes / 60);
  if (hours < 24) return `${hours}h`;
  return `${Math.round(hours / 24)}d`;
}

/** 把未知 window key 转成可读标签 */
export function prettifyWindow(key: string | undefined): string {
  if (!key) return "";
  return key.replace(/[_-]+/g, " ").trim();
}

export function maskKeyName(name: string): string {
  return name.replace(/\.json$/i, "");
}
