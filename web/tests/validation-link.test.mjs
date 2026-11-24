import assert from "node:assert/strict";
import { test } from "node:test";
import { extractGoogleValidationLink, googleAccountsUrl } from "../src/lib/validation-link.ts";

test("extracts trusted Google account verification links from error messages", () => {
  const url = "https://accounts.google.com/signin/continue?test=1";
  assert.deepEqual(extractGoogleValidationLink(`Verify this account. (${url})`), {
    text: "Verify this account.",
    url,
  });
});

test("rejects untrusted verification URLs", () => {
  for (const unsafe of [
    "javascript:alert(1)",
    "http://accounts.google.com/signin",
    "https://accounts.google.com.example.org/signin",
    "https://elsewhere.test@accounts.google.com/signin",
    "https://accounts.google.com:8080/signin",
  ]) {
    assert.equal(googleAccountsUrl(unsafe), undefined);
    assert.deepEqual(extractGoogleValidationLink(`Verify (${unsafe})`), {
      text: `Verify (${unsafe})`,
    });
  }
});
