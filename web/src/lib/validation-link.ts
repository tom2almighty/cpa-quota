const GOOGLE_ACCOUNTS_URL = /\(?(https:\/\/accounts\.google\.com\/[^\s)]+)\)?/i;

/** 校验是否为可信的 Google 账号验证链接，拒绝带凭据、端口或伪造域名的地址。 */
export function googleAccountsUrl(value: unknown): string | undefined {
  if (typeof value !== "string") return undefined;
  try {
    const parsed = new URL(value.trim());
    if (parsed.protocol === "https:" && parsed.host === "accounts.google.com" && !parsed.username && !parsed.password) {
      return parsed.href;
    }
  } catch {
    return undefined;
  }
  return undefined;
}

/** 从插件错误信息中拆分出可信的 Google 账号验证链接与展示文案。 */
export function extractGoogleValidationLink(message: string): { text: string; url?: string } {
  const match = message.match(GOOGLE_ACCOUNTS_URL);
  if (!match) return { text: message };
  const url = googleAccountsUrl(match[1]);
  if (!url) return { text: message };
  const text = message
    .replace(match[0], "")
    .replace(/\s{2,}/g, " ")
    .trim();
  return { text: text || message, url };
}
