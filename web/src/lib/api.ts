import { resolveApiBase } from "@/lib/auth";
import type { Credential, CredentialsResponse, QuotaResetResponse, QuotaResponse } from "@/types";

export const QUOTA_PLUGIN_ID = "cpa-quota";
const PAGE_SIZE = 200;
const MAX_PAGES = 20;

/** 401：密钥缺失或无效 */
export class UnauthorizedError extends Error {
  constructor() {
    super("management key required");
    this.name = "UnauthorizedError";
  }
}

async function request<T>(key: string, path: string, init: RequestInit = {}): Promise<T> {
  // 没有密钥就不发请求：CPA 会把无鉴权请求计入认证失败次数，连续 5 次会封禁该 IP
  if (!key) throw new UnauthorizedError();

  const headers = new Headers(init.headers);
  headers.set("Authorization", `Bearer ${key}`);
  if (init.body && !headers.has("Content-Type")) headers.set("Content-Type", "application/json");

  const res = await fetch(`${resolveApiBase()}${path}`, { ...init, headers });
  if (res.status === 401) throw new UnauthorizedError();
  if (!res.ok) {
    let message = `HTTP ${res.status}`;
    try {
      const body = (await res.json()) as { error?: string; message?: string };
      if (body.message) message = body.message;
      else if (body.error) message = body.error;
    } catch {
      // 非 JSON 错误体，保留 HTTP 状态
    }
    throw new Error(message);
  }
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

/** 拉取全部凭据；CPA 分页时顺序补齐。 */
export async function fetchCredentials(key: string): Promise<Credential[]> {
  const files: Credential[] = [];
  for (let page = 1; page <= MAX_PAGES; page++) {
    const res = await request<CredentialsResponse>(
      key,
      `/v8/management/credentials?page=${page}&page_size=${PAGE_SIZE}`,
    );
    files.push(...(res.files ?? []));
    if (!res.has_more) break;
  }
  return files;
}

/** 只保留由本插件提供额度的凭据。v8 凭据会直接返回 quota_plugin_id。 */
export function isQuotaCredential(credential: Credential): boolean {
  return Boolean(credential.auth_index) && credential.quota_plugin_id === QUOTA_PLUGIN_ID;
}

export function fetchQuota(key: string, authIndex: string): Promise<QuotaResponse> {
  return request<QuotaResponse>(key, `/v8/management/plugins/${QUOTA_PLUGIN_ID}/quota`, {
    method: "POST",
    body: JSON.stringify({ auth_index: authIndex }),
  });
}

export function resetQuota(key: string, authIndex: string): Promise<QuotaResetResponse> {
  return request<QuotaResetResponse>(
    key,
    `/v8/management/plugins/${QUOTA_PLUGIN_ID}/quota?auth_index=${encodeURIComponent(authIndex)}`,
    { method: "DELETE" },
  );
}

/**
 * 查询缓存按密钥分域：密钥变了缓存不能复用，但也不能把密钥本身写进
 * queryKey（会被 devtools/缓存序列化）。这里只留一个稳定的短指纹。
 */
export function keyFingerprint(key: string): string {
  let hash = 0x811c9dc5;
  for (let i = 0; i < key.length; i++) {
    hash ^= key.charCodeAt(i);
    hash = Math.imul(hash, 0x01000193) >>> 0;
  }
  return hash.toString(16);
}
