import assert from "node:assert/strict";
import { test } from "node:test";
import {
  CLAIM_RETRY_MS,
  ClaudeGrantError,
  claimRequestId,
  clearClaimRequestId,
  defaultGrantId,
  grantBlockReason,
  parseClaudeResetStatus,
  pendingClaim,
} from "../src/lib/claude-grants.ts";

const grant = {
  id: "grant-a",
  label: " Weekly ",
  resets_total: 2,
  resets_left: 1,
  starts_at: "2026-11-01T00:00:00Z",
  ends_at: "2026-12-01T00:00:00Z",
  clears: ["five_hour", "seven_day"],
  paused: false,
  usable_now: true,
  use_requires_limit: true,
  percent_used: { five_hour: 10, seven_day: 20 },
  type: "weekly",
  gives_back_weekly: true,
};

const block = (overrides = {}) => ({
  eligible: true,
  at_limit: true,
  next_grant_id: "grant-a",
  cooldown_until: null,
  grants: [grant],
  ...overrides,
});

test("parses the cedar_ember block and keeps grant metadata", () => {
  const status = parseClaudeResetStatus(block());
  assert.ok(status);
  assert.equal(status.eligible, true);
  assert.equal(status.nextGrantId, "grant-a");
  assert.equal(status.grants.length, 1);
  const parsed = status.grants[0];
  assert.equal(parsed.label, "Weekly");
  assert.equal(parsed.kind, "weekly");
  assert.equal(parsed.resetsLeft, 1);
  assert.equal(parsed.givesBackWeekly, true);
  assert.deepEqual(parsed.clears, ["five_hour", "seven_day"]);
});

test("rejects malformed blocks, grants, duplicates and timestamps", () => {
  for (const invalid of [
    null,
    {},
    block({ grants: "nope" }),
    block({ grants: [{ ...grant, id: "Bad ID" }] }),
    block({ grants: [{ ...grant, resets_total: 1.5 }] }),
    block({ grants: [{ ...grant, resets_left: 3 }] }),
    block({ grants: [{ ...grant, ends_at: "not-a-date" }] }),
    block({ grants: [grant, grant] }),
    block({ at_limit: "yes" }),
    block({ cooldown_until: "not-a-date" }),
  ]) {
    assert.equal(parseClaudeResetStatus(invalid), null);
  }
});

test("blocks unusable grants and honors gives_back_weekly without usage", () => {
  const status = parseClaudeResetStatus(block());
  const parsed = status.grants[0];
  assert.equal(grantBlockReason(status, parsed, Date.parse("2026-11-15T00:00:00Z")), null);

  const notLimited = parseClaudeResetStatus(block({ at_limit: false }));
  assert.equal(grantBlockReason(notLimited, notLimited.grants[0]), "not_limited");

  const noUsage = parseClaudeResetStatus(
    block({ grants: [{ ...grant, percent_used: { five_hour: 0, seven_day: 0 } }] }),
  );
  assert.equal(grantBlockReason(noUsage, noUsage.grants[0]), "no_usage");

  const expired = parseClaudeResetStatus(block());
  assert.equal(grantBlockReason(expired, expired.grants[0], Date.parse("2026-12-02T00:00:00Z")), "expired");

  const cooling = parseClaudeResetStatus(block({ cooldown_until: "2026-11-20T00:00:00Z" }));
  assert.equal(grantBlockReason(cooling, cooling.grants[0], Date.parse("2026-11-15T00:00:00Z")), "cooldown");

  const ineligible = parseClaudeResetStatus(block({ eligible: false }));
  assert.equal(grantBlockReason(ineligible, ineligible.grants[0]), "ineligible");
});

test("prefers the upstream hint and falls back to the first usable grant", () => {
  const second = { ...grant, id: "grant-b", label: "B" };
  const hinted = parseClaudeResetStatus(block({ grants: [grant, second], next_grant_id: "grant-b" }));
  assert.equal(defaultGrantId(hinted, Date.parse("2026-11-15T00:00:00Z")), "grant-b");

  const exhaustedHint = parseClaudeResetStatus(
    block({ grants: [{ ...grant, id: "grant-b", resets_left: 0 }, grant], next_grant_id: "grant-b" }),
  );
  assert.equal(defaultGrantId(exhaustedHint, Date.parse("2026-11-15T00:00:00Z")), "grant-a");
});

test("claim journal reuses the request id and refuses to switch grants", () => {
  const now = Date.parse("2026-11-15T00:00:00Z");
  clearClaimRequestId("idx");
  const first = claimRequestId("idx", "grant-a", now);
  assert.equal(first.retry, false);
  assert.match(first.requestId, /^[A-Za-z0-9_-]{1,64}$/);

  const retry = claimRequestId("idx", "grant-a", now + 1000);
  assert.deepEqual(retry, { requestId: first.requestId, retry: true });
  assert.equal(pendingClaim("idx", now + 1000).grantId, "grant-a");

  assert.throws(
    () => claimRequestId("idx", "grant-b", now + 2000),
    (error) => {
      assert.ok(error instanceof ClaudeGrantError);
      assert.equal(error.code, "pending_other_grant");
      return true;
    },
  );

  const afterTtl = claimRequestId("idx", "grant-b", now + CLAIM_RETRY_MS + 1);
  assert.equal(afterTtl.retry, false);
  assert.notEqual(afterTtl.requestId, first.requestId);

  clearClaimRequestId("idx");
  assert.equal(pendingClaim("idx"), null);
});
