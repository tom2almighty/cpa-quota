import i18n from "@/i18n";
import type { Credential, QuotaBucket, QuotaResponse } from "@/types";

const HOUR_MS = 3_600_000;

export function providerLabel(provider: string): string {
  switch (provider.toLowerCase()) {
    case "claude":
    case "anthropic":
      return "Claude";
    case "codex":
      return "Codex";
    case "antigravity":
      return "Antigravity";
    case "kimi":
    case "kimi-ai":
      return "Kimi";
    case "xai":
      return "xAI";
    case "devin":
      return "Devin";
    case "meta":
      return "Meta";
    default:
      return provider;
  }
}

export function credentialName(credential: Credential): string {
  return credential.email || credential.label || credential.account || credential.name.replace(/\.json$/i, "");
}

function prettifyWindow(key: string | undefined): string {
  if (!key) return "";
  return key.replace(/[_-]+/g, " ").trim();
}

/** 已知 window key 走 i18n，未知 key 转成可读文案。 */
export function bucketLabel(bucket: QuotaBucket): string {
  const key = (bucket.window ?? "").trim();
  if (key) {
    const translated = i18n.t(`window.${key}`, { defaultValue: "" });
    if (translated) return translated;
  }
  return prettifyWindow(key) || i18n.t("common.unknown");
}

/** 上游描述里的固定句式转成当前语言，金额等自由文本原样保留。 */
export function detailText(description: string): string {
  const models = description.match(/^Models within this group:\s*(.+)$/i);
  if (models) return i18n.t("detail.group_models", { models: models[1] });
  const depleted = description.match(/^(.*?)\s*·\s*Weekly limit exhausted$/i);
  if (depleted) return `${depleted[1]} · ${i18n.t("detail.weekly_depleted")}`;
  if (/^Weekly limit exhausted$/i.test(description)) return i18n.t("detail.weekly_depleted");
  const hours = description.match(/^(\d+) hour window$/i);
  if (hours) return i18n.t("detail.hours_window", { n: hours[1] });
  const minutes = description.match(/^(\d+) min window$/i);
  if (minutes) return i18n.t("detail.minutes_window", { n: minutes[1] });
  return description;
}

export type BucketTone = "success" | "warning" | "danger";

export function bucketTone(fraction: number): BucketTone {
  const percent = Math.round(fraction * 100);
  if (percent <= 0) return "danger";
  if (percent <= 20) return "warning";
  return "success";
}

export function bucketPercent(bucket: QuotaBucket): number {
  return Math.round(Math.max(0, Math.min(1, bucket.remainingFraction)) * 100);
}

/** 从 window 标识或描述推断额度窗口长度；无法推断时返回 null。 */
export function windowPeriodHours(window: string | undefined, description?: string): number | null {
  const key = (window ?? "").toLowerCase().trim();
  if (!key) return null;
  if (key.includes("five_hour") || key.includes("five-hour") || key.includes("5h") || key.includes("5_hour")) {
    return 5;
  }
  if (key.includes("weekly") || key === "week" || key.includes("seven_day") || key.includes("seven-day")) {
    return 7 * 24;
  }
  if (key.includes("monthly") || key === "month") return 30 * 24;
  if (key === "daily" || key === "day") return 24;
  const hours = key.match(/^hours?_(\d+(?:\.\d+)?)$/);
  if (hours) return Number(hours[1]);
  const days = key.match(/^days?_(\d+(?:\.\d+)?)$/);
  if (days) return Number(days[1]) * 24;
  if (key.includes("session")) {
    const match = (description ?? "").match(/(\d+)\s*(hour|h\b|min)/i);
    if (match) {
      const amount = Number(match[1]);
      return /min/i.test(match[2]) ? amount / 60 : amount;
    }
  }
  return null;
}

export type QuotaViewItem = {
  credential: Credential;
  quota?: QuotaResponse;
};

export type TimelineLane = {
  credential: Credential;
  account: string;
  provider: string;
  label: string;
  detail?: string;
  /** API 报告的当前窗口剩余百分比。 */
  remaining: number;
  tone: BucketTone;
  anchorMs: number;
  periodHours: number;
};

export type TimelineWindow = {
  id: string;
  startMs: number;
  endMs: number;
  leftPercent: number;
  widthPercent: number;
  state: "past" | "live" | "next";
  remaining: number | null;
};

type Candidate = {
  bucket: QuotaBucket;
  label: string;
  detail?: string;
  remaining: number;
  tone: BucketTone;
  anchorMs: number;
  periodHours: number;
};

/**
 * 每个凭据选一个窗口画成泳道：优先选不超过可见跨度、周期最长的窗口
 * （周视图看 7 天窗口，5 小时视图看 5 小时窗口），并列时取最近重置。
 */
export function buildTimelineLane(item: QuotaViewItem, maxPeriodHours: number): TimelineLane | null {
  const provider = (item.credential.provider ?? item.credential.type ?? "").toLowerCase();
  const candidates: Candidate[] = [];
  for (const group of item.quota?.groups ?? []) {
    for (const bucket of group.buckets ?? []) {
      if (!bucket.resetTime) continue;
      const anchor = new Date(bucket.resetTime);
      if (Number.isNaN(anchor.getTime())) continue;
      const periodHours = windowPeriodHours(bucket.window, bucket.description);
      if (periodHours === null || periodHours <= 0) continue;
      candidates.push({
        bucket,
        label: bucketLabel(bucket),
        detail: bucket.description ? detailText(bucket.description) : undefined,
        remaining: bucketPercent(bucket),
        tone: bucketTone(bucket.remainingFraction),
        anchorMs: anchor.getTime(),
        periodHours,
      });
    }
  }
  if (candidates.length === 0) return null;

  const fitting = candidates.filter((candidate) => candidate.periodHours <= maxPeriodHours);
  const pool = fitting.length > 0 ? fitting : candidates;
  const chosen = pool.reduce((best, candidate) => {
    if (candidate.periodHours !== best.periodHours) {
      return candidate.periodHours > best.periodHours ? candidate : best;
    }
    return candidate.anchorMs < best.anchorMs ? candidate : best;
  });

  return {
    credential: item.credential,
    account: credentialName(item.credential),
    provider,
    label: chosen.label,
    detail: chosen.detail,
    remaining: chosen.remaining,
    tone: chosen.tone,
    anchorMs: chosen.anchorMs,
    periodHours: chosen.periodHours,
  };
}

function windowsIn(anchorMs: number, periodMs: number, fromMs: number, toMs: number) {
  if (!(periodMs > 0) || !(toMs > fromMs)) return [];
  const count = Math.ceil((toMs - fromMs) / periodMs) + 2;
  if (count > 1000) return [];
  let end = anchorMs + Math.ceil((fromMs - anchorMs) / periodMs) * periodMs;
  const out: { startMs: number; endMs: number }[] = [];
  while (end - periodMs < toMs) {
    out.push({ startMs: end - periodMs, endMs: end });
    end += periodMs;
  }
  return out;
}

/** 把一条泳道的周期窗口投影到可见时间范围。 */
export function projectTimelineWindows(
  lane: TimelineLane,
  spanStartMs: number,
  spanEndMs: number,
  nowMs: number,
): TimelineWindow[] {
  const span = spanEndMs - spanStartMs;
  if (span <= 0) return [];
  const periodMs = lane.periodHours * HOUR_MS;
  const toPercent = (ms: number) => ((ms - spanStartMs) / span) * 100;

  return windowsIn(lane.anchorMs, periodMs, spanStartMs, spanEndMs)
    .map((window, index): TimelineWindow | null => {
      const left = Math.max(0, toPercent(window.startMs));
      const right = Math.min(100, toPercent(window.endMs));
      if (right <= 0 || left >= 100 || right <= left) return null;
      const state: TimelineWindow["state"] = window.endMs <= nowMs ? "past" : window.startMs <= nowMs ? "live" : "next";
      return {
        id: `${lane.credential.auth_index ?? lane.credential.id}-${index}`,
        startMs: window.startMs,
        endMs: window.endMs,
        leftPercent: left,
        widthPercent: right - left,
        state,
        remaining: state === "live" && window.endMs === lane.anchorMs ? lane.remaining : null,
      };
    })
    .filter((window): window is TimelineWindow => window !== null);
}
