/**
 * Claude (Anthropic) reset-grant contract, following the `cedar_ember` block in
 * `api/oauth/usage?cedar_ember=1&skip_spend=1`. Kept free of network imports so
 * the parsing and claim journal can be tested directly under Node.
 */

export const CLAUDE_RESET_RESULTS = [
  "reset",
  "already_used",
  "not_limited",
  "cooldown",
  "ineligible",
  "unavailable",
] as const;

export type ClaudeResetResult = (typeof CLAUDE_RESET_RESULTS)[number];

export type ClaudeResetGrant = {
  id: string;
  label: string;
  resetsTotal: number;
  resetsLeft: number;
  startsAt: string | null;
  endsAt: string | null;
  clears: string[];
  paused: boolean;
  usableNow: boolean;
  useRequiresLimit: boolean;
  percentUsed: Record<string, number>;
  kind: "weekly" | "session" | "full" | null;
  givesBackWeekly: boolean;
};

export type ClaudeResetStatus = {
  eligible: boolean;
  atLimit: boolean;
  nextGrantId: string | null;
  cooldownUntil: string | null;
  grants: ClaudeResetGrant[];
};

export type ClaudeGrantBlock =
  | "ineligible"
  | "paused"
  | "not_usable"
  | "exhausted"
  | "not_limited"
  | "no_usage"
  | "not_started"
  | "expired"
  | "cooldown";

export type ClaudeGrantErrorCode =
  | "auth"
  | "rate_limited"
  | "upstream"
  | "malformed"
  | "unknown_outcome"
  | "pending_other_grant";

export class ClaudeGrantError extends Error {
  readonly code: ClaudeGrantErrorCode;

  constructor(code: ClaudeGrantErrorCode) {
    super(code);
    this.name = "ClaudeGrantError";
    this.code = code;
  }
}

const GRANT_ID_RE = /^[a-z0-9_-]{1,40}$/;
const REQUEST_ID_RE = /^[A-Za-z0-9_-]{1,64}$/;
export const CLAUDE_ORGANIZATION_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function isCount(value: unknown): value is number {
  return typeof value === "number" && Number.isInteger(value) && value >= 0;
}

/** Optional ISO timestamp: absent/null -> null, anything unparsable rejects the block. */
function optionalTimestamp(value: unknown): string | null | undefined {
  if (value === undefined || value === null) return null;
  if (typeof value !== "string" || Number.isNaN(Date.parse(value))) return undefined;
  return value;
}

function optionalBoolean(value: unknown, fallback: boolean): boolean | undefined {
  if (value === undefined || value === null) return fallback;
  return typeof value === "boolean" ? value : undefined;
}

/** Upstream display text, stripped of control characters and bounded. */
function safeLabel(value: unknown): string {
  if (typeof value !== "string") return "";
  let out = "";
  for (const char of value) {
    const code = char.codePointAt(0) ?? 0;
    out += code < 0x20 || (code >= 0x7f && code <= 0x9f) ? " " : char;
  }
  return out.replace(/\s+/g, " ").trim().slice(0, 120);
}

function parseClears(value: unknown): string[] | undefined {
  if (value === undefined || value === null) return [];
  if (!Array.isArray(value)) return undefined;
  return value.filter((item): item is string => typeof item === "string" && item.trim() !== "");
}

/** Invalid percentages reject the map so a refill cannot be mistaken for zero usage. */
function parsePercentUsed(value: unknown): Record<string, number> {
  if (!isRecord(value)) return {};
  const out: Record<string, number> = {};
  for (const [window, percent] of Object.entries(value)) {
    if (typeof percent !== "number" || !Number.isFinite(percent) || percent < 0 || percent > 100) {
      return {};
    }
    out[window] = percent;
  }
  return out;
}

function parseGrant(value: unknown): ClaudeResetGrant | null {
  if (!isRecord(value)) return null;
  const { id, resets_total: total, resets_left: left } = value;
  if (typeof id !== "string" || !GRANT_ID_RE.test(id)) return null;
  if (!isCount(total) || !isCount(left) || left > total) return null;
  const startsAt = optionalTimestamp(value.starts_at);
  const endsAt = optionalTimestamp(value.ends_at);
  const clears = parseClears(value.clears);
  const paused = optionalBoolean(value.paused, false);
  // Missing usability flags default to the refusing side.
  const usableNow = optionalBoolean(value.usable_now, false);
  const useRequiresLimit = optionalBoolean(value.use_requires_limit, true);
  const givesBackWeekly = optionalBoolean(value.gives_back_weekly, false);
  if (
    startsAt === undefined ||
    endsAt === undefined ||
    clears === undefined ||
    paused === undefined ||
    usableNow === undefined ||
    useRequiresLimit === undefined ||
    givesBackWeekly === undefined
  ) {
    return null;
  }
  return {
    id,
    label: safeLabel(value.label),
    resetsTotal: total,
    resetsLeft: left,
    startsAt,
    endsAt,
    clears,
    paused,
    usableNow,
    useRequiresLimit,
    percentUsed: parsePercentUsed(value.percent_used),
    kind: value.type === "weekly" || value.type === "session" || value.type === "full" ? value.type : null,
    givesBackWeekly,
  };
}

/** One malformed grant, a duplicate id or a bad timestamp rejects the whole block. */
export function parseClaudeResetStatus(value: unknown): ClaudeResetStatus | null {
  if (!isRecord(value) || typeof value.eligible !== "boolean") return null;
  const rawGrants = value.grants ?? [];
  if (!Array.isArray(rawGrants)) return null;
  const grants: ClaudeResetGrant[] = [];
  const seen = new Set<string>();
  for (const raw of rawGrants) {
    const grant = parseGrant(raw);
    if (!grant || seen.has(grant.id)) return null;
    seen.add(grant.id);
    grants.push(grant);
  }
  const atLimit = optionalBoolean(value.at_limit, false);
  const cooldownUntil = optionalTimestamp(value.cooldown_until);
  if (atLimit === undefined || cooldownUntil === undefined) return null;
  const next = value.next_grant_id;
  return {
    eligible: value.eligible,
    atLimit,
    cooldownUntil,
    grants,
    nextGrantId: typeof next === "string" && seen.has(next) ? next : null,
  };
}

/** Why a grant cannot be spent right now, or null when it can. */
export function grantBlockReason(
  status: ClaudeResetStatus,
  grant: ClaudeResetGrant,
  now: number = Date.now(),
): ClaudeGrantBlock | null {
  if (!status.eligible) return "ineligible";
  if (grant.paused) return "paused";
  if (!grant.usableNow) return "not_usable";
  if (grant.resetsLeft <= 0) return "exhausted";
  if (grant.useRequiresLimit && !status.atLimit) return "not_limited";
  if (grantHasNoUsage(grant)) return "no_usage";
  if (grant.startsAt && Date.parse(grant.startsAt) > now) return "not_started";
  if (grant.endsAt && Date.parse(grant.endsAt) <= now) return "expired";
  if (status.cooldownUntil && Date.parse(status.cooldownUntil) > now) return "cooldown";
  return null;
}

/** Missing usage is unknown; only an all-zero refill is provably wasted. */
export function grantHasNoUsage(grant: ClaudeResetGrant): boolean {
  const percentages = Object.values(grant.percentUsed);
  return grant.givesBackWeekly && percentages.length > 0 && percentages.every((percent) => percent === 0);
}

/** Preferred grant for an automatic selection: the upstream hint, else the first usable one. */
export function defaultGrantId(status: ClaudeResetStatus, now: number = Date.now()): string | null {
  const usable = status.grants.filter((grant) => grantBlockReason(status, grant, now) === null);
  const hinted = usable.find((grant) => grant.id === status.nextGrantId);
  return hinted?.id ?? usable[0]?.id ?? null;
}

export type PendingClaim = { grantId: string; requestId: string; at: number };

export const CLAIM_RETRY_MS = 10 * 60 * 1000;

const pendingClaims = new Map<string, PendingClaim>();

function newRequestId(): string {
  const bytes = new Uint8Array(16);
  const cryptoApi = globalThis.crypto;
  if (cryptoApi?.getRandomValues) {
    cryptoApi.getRandomValues(bytes);
  } else {
    for (let i = 0; i < bytes.length; i++) bytes[i] = Math.floor(Math.random() * 256);
  }
  let id = "";
  for (const byte of bytes) id += byte.toString(16).padStart(2, "0");
  return id;
}

/** Reuse the request id across retries so an ambiguous claim cannot spend twice. */
export function claimRequestId(
  authIndex: string,
  grantId: string,
  now: number = Date.now(),
): { requestId: string; retry: boolean } {
  const existing = pendingClaims.get(authIndex);
  if (existing && now - existing.at < CLAIM_RETRY_MS) {
    if (existing.grantId !== grantId) throw new ClaudeGrantError("pending_other_grant");
    return { requestId: existing.requestId, retry: true };
  }
  const requestId = newRequestId();
  if (!REQUEST_ID_RE.test(requestId)) throw new ClaudeGrantError("malformed");
  pendingClaims.set(authIndex, { grantId, requestId, at: now });
  return { requestId, retry: false };
}

export function pendingClaim(authIndex: string, now: number = Date.now()): PendingClaim | null {
  const existing = pendingClaims.get(authIndex);
  if (!existing) return null;
  if (now - existing.at >= CLAIM_RETRY_MS) {
    pendingClaims.delete(authIndex);
    return null;
  }
  return existing;
}

export function clearClaimRequestId(authIndex: string): void {
  pendingClaims.delete(authIndex);
}

export function isClaudeResetResult(value: unknown): value is ClaudeResetResult {
  return typeof value === "string" && (CLAUDE_RESET_RESULTS as readonly string[]).includes(value);
}
