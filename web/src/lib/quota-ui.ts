import i18n from "@/i18n";
import type { Credential, QuotaBucket, QuotaResponse } from "@/types";

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

export type ResetEvent = {
  id: string;
  at: Date;
  credential: Credential;
  account: string;
  provider: string;
  label: string;
  detail?: string;
  percent: number;
  tone: BucketTone;
};

export type QuotaViewItem = {
  credential: Credential;
  quota?: QuotaResponse;
};

/** 把一个凭据的所有带 resetTime 的额度桶摊平成重置事件。 */
export function buildResetEvents(items: QuotaViewItem[]): ResetEvent[] {
  const events: ResetEvent[] = [];
  for (const item of items) {
    const provider = (item.credential.provider ?? item.credential.type ?? "").toLowerCase();
    const account = credentialName(item.credential);
    for (const [groupIndex, group] of (item.quota?.groups ?? []).entries()) {
      for (const [bucketIndex, bucket] of (group.buckets ?? []).entries()) {
        if (!bucket.resetTime) continue;
        const at = new Date(bucket.resetTime);
        if (Number.isNaN(at.getTime())) continue;
        const detail = bucket.description ? detailText(bucket.description) : undefined;
        events.push({
          id: `${item.credential.auth_index ?? item.credential.id}-${groupIndex}-${bucketIndex}-${at.getTime()}`,
          at,
          credential: item.credential,
          account,
          provider,
          label: bucketLabel(bucket),
          detail,
          percent: bucketPercent(bucket),
          tone: bucketTone(bucket.remainingFraction),
        });
      }
    }
  }
  return events.sort((a, b) => a.at.getTime() - b.at.getTime());
}
