import { apiCall } from "./api";
import {
  CLAUDE_ORGANIZATION_RE,
  ClaudeGrantError,
  type ClaudeResetResult,
  type ClaudeResetStatus,
  clearClaimRequestId,
  isClaudeResetResult,
  parseClaudeResetStatus,
} from "./claude-grants";

const CLAUDE_API_ORIGIN = "https://api.anthropic.com";
const CLAUDE_RESET_STATUS_PATH = "/api/oauth/usage?cedar_ember=1&skip_spend=1";
const CLAUDE_PROFILE_PATH = "/api/oauth/profile";
const CLAUDE_RESET_GRANT_PROGRAM = "cedar_ember";
const GRANT_ID_RE = /^[a-z0-9_-]{1,40}$/;
const REQUEST_ID_RE = /^[A-Za-z0-9_-]{1,64}$/;

// Keep in sync with internal/provider/claude.go claudeUserAgent.
const CLAUDE_USER_AGENT = "claude-cli/2.1.321 (external, cli)";

const CLAUDE_HEADERS: Record<string, string> = {
  "User-Agent": CLAUDE_USER_AGENT,
  Authorization: "Bearer $TOKEN$",
  "Content-Type": "application/json",
  "anthropic-beta": "oauth-2025-04-20",
};

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function decodeBody(raw: string | undefined): unknown {
  if (!raw) return null;
  try {
    return JSON.parse(raw) as unknown;
  } catch {
    return null;
  }
}

function readError(status: number): ClaudeGrantError {
  if (status === 401 || status === 403) return new ClaudeGrantError("auth");
  if (status === 429) return new ClaudeGrantError("rate_limited");
  return new ClaudeGrantError("upstream");
}

async function readAccount(key: string, authIndex: string, path: string): Promise<Record<string, unknown>> {
  const response = await apiCall(key, {
    auth_index: authIndex,
    method: "GET",
    url: CLAUDE_API_ORIGIN + path,
    header: CLAUDE_HEADERS,
  });
  if (response.status_code < 200 || response.status_code >= 300) {
    throw readError(response.status_code);
  }
  const body = decodeBody(response.body);
  if (!isRecord(body)) throw new ClaudeGrantError("malformed");
  return body;
}

export async function readClaudeResetGrants(key: string, authIndex: string): Promise<ClaudeResetStatus> {
  const body = await readAccount(key, authIndex, CLAUDE_RESET_STATUS_PATH);
  const status = parseClaudeResetStatus(body.cedar_ember);
  if (!status) throw new ClaudeGrantError("malformed");
  return status;
}

export async function readClaudeOrganization(key: string, authIndex: string): Promise<string> {
  const body = await readAccount(key, authIndex, CLAUDE_PROFILE_PATH);
  const organization = isRecord(body.organization) ? body.organization.uuid : undefined;
  if (typeof organization !== "string" || !CLAUDE_ORGANIZATION_RE.test(organization)) {
    throw new ClaudeGrantError("malformed");
  }
  return organization.toLowerCase();
}

/**
 * Claim one grant. Only 401/403/429 and a recognized terminal result prove the
 * request settled; anything else may have reached Anthropic, so the claim
 * journal keeps the request id for a safe retry.
 */
export async function claimClaudeGrant(
  key: string,
  params: { authIndex: string; organization: string; grantId: string; requestId: string },
): Promise<ClaudeResetResult> {
  const { authIndex, organization, grantId, requestId } = params;
  if (!CLAUDE_ORGANIZATION_RE.test(organization) || !GRANT_ID_RE.test(grantId) || !REQUEST_ID_RE.test(requestId)) {
    throw new ClaudeGrantError("malformed");
  }

  let response: Awaited<ReturnType<typeof apiCall>>;
  try {
    response = await apiCall(key, {
      auth_index: authIndex,
      method: "POST",
      url: `${CLAUDE_API_ORIGIN}/api/organizations/${organization}/reset_rate_limits`,
      header: CLAUDE_HEADERS,
      data: JSON.stringify({
        program: CLAUDE_RESET_GRANT_PROGRAM,
        grant_id: grantId,
        request_id: requestId,
      }),
    });
  } catch {
    throw new ClaudeGrantError("unknown_outcome");
  }

  if (response.status_code === 429 || response.status_code === 401 || response.status_code === 403) {
    clearClaimRequestId(authIndex);
    throw readError(response.status_code);
  }

  const body = decodeBody(response.body);
  const result = isRecord(body) ? body.result : undefined;
  if (response.status_code >= 200 && response.status_code < 300 && isClaudeResetResult(result)) {
    clearClaimRequestId(authIndex);
    return result;
  }
  throw new ClaudeGrantError("unknown_outcome");
}
