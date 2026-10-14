import assert from "node:assert/strict";
import { test } from "node:test";
import {
  financialSummary,
  financialCredits,
  financialReset,
  validateFinancialOverview,
} from "./financial-summary.ts";
const now = Date.parse("2026-10-11T00:00:00Z");
const account = (ref, used, credits, financialEligible = true) => ({
  ref,
  name: ref,
  financialEligible,
  financialSummary: {
    version: 1,
    receivedAt: new Date(now).toISOString(),
    observation: {
      state: "AVAILABLE",
      sequence: 1,
      ...(used === undefined ? {} : { windows: [{ usedPercent: used }] }),
      ...(credits ? { credits } : {}),
    },
  },
});

test("0/1/2/3 and full set beyond first page, stable ties", () => {
  for (let count = 0; count <= 3; count++)
    assert.equal(
      financialSummary(
        Array.from({ length: count }, (_, i) => account(String(i), i)),
        true,
        true,
        now,
      ).accounts.length,
      count,
    );
  const accounts = Array.from({ length: 70 }, (_, i) =>
    account(String(i).padStart(3, "0"), i),
  );
  accounts[69] = account("paid", 100, {
    hasCredits: true,
    unlimited: false,
    balance: "59999.999999999999999999",
  });
  const result = financialSummary(accounts, true, true, now);
  assert.deepEqual(
    result.accounts.map((item) => item.account.ref),
    ["paid", "068", "067"],
  );
  assert.equal(result.exhausted, 1);
  assert.equal(
    financialCredits(accounts[69], true),
    "Кредиты: " + accounts[69].financialSummary.observation.credits.balance,
  );
  assert.deepEqual(
    financialSummary(
      [account("b", 30), account("a", 30)],
      true,
      true,
      now,
    ).accounts.map((item) => item.account.ref),
    ["a", "b"],
  );
  assert.deepEqual(
    financialSummary(
      [account("pac_a0001", 30), account("pac_A0001", 30)],
      true,
      true,
      now,
    ).accounts.map((item) => item.account.ref),
    ["pac_A0001", "pac_a0001"],
  );
});
test("unknown, unlimited credits, upstream restrictions and stale do not invent included balance", () => {
  const unknown = account("unknown", undefined, {
    hasCredits: true,
    unlimited: true,
  });
  assert.equal(
    financialSummary([unknown], true, true, now).accounts[0].remaining,
    undefined,
  );
  assert.equal(
    financialSummary([account("zero", 100)], false, true, now).exhausted,
    0,
  );
  assert.equal(
    financialSummary([account("old", 100)], true, true, now + 300001)
      .accounts[0].remaining,
    undefined,
  );
  assert.equal(financialSummary([unknown], true, false, now).complete, false);
  assert.match(financialCredits(unknown, false), /недоступно/);
  assert.equal(
    financialCredits(
      account("pac_unlimited", 100, {
        hasCredits: true,
        unlimited: true,
        balance: "59999.1234567890123456789",
      }),
      true,
    ),
    "Кредиты: без ограничения; баланс: 59999.1234567890123456789",
  );
  assert.match(
    financialCredits(
      account("paid", 100, { hasCredits: true, unlimited: false }),
      true,
    ),
    /есть; баланс недоступен/,
  );
  assert.equal(financialReset(undefined), undefined);
  assert.equal(financialReset(-1), undefined);
  assert.ok(financialReset(0));
});
test("scope/resource filtering precedes counts and ranking", () => {
  const result = financialSummary(
    [account("hidden", 100, undefined, false), account("visible", 10)],
    true,
    true,
    now,
  );
  assert.equal(result.total, 1);
  assert.equal(result.exhausted, 0);
  assert.equal(result.accounts[0].account.ref, "visible");
});

test("protected bounded summary retains complete counts and rejects malformed presence", () => {
  const value = {
    accounts: [
      account("acct_a001", 100),
      account("acct_b001", 10),
      account("acct_c001", undefined),
    ].map(({ ref, name, financialSummary }) => ({
      ref,
      name,
      financialSummary,
    })),
    total: 6200,
    exhausted: 10,
    unknown: 5,
    observedAt: new Date(now).toISOString(),
    validUntil: new Date(now + 300000).toISOString(),
  };
  const overview = validateFinancialOverview(value);
  const result = financialSummary(
    overview.accounts.map((item) => ({ ...item, financialEligible: true })),
    true,
    true,
    now,
    overview,
  );
  assert.equal(result.total, 6200);
  assert.equal(result.exhausted, 10);
  assert.equal(result.unknown, 5);
  assert.equal(
    financialSummary([], true, true, now + 300001, overview).unknown,
    6200,
  );
  for (const mutate of [
    (v) => (v.accounts[0].financialSummary.observation.windows = [{}]),
    (v) =>
      (v.accounts[0].financialSummary.observation.credits = {
        hasCredits: null,
      }),
    (v) => (v.accounts[0].financialSummary.observation.accountId = "private"),
    (v) => (v.total = 2),
    (v) => v.accounts.push(v.accounts[0]),
    (v) => (v.accounts[0].ref = ""),
    (v) => (v.accounts[0].ref = "acct/foreign"),
    (v) => (v.accounts[0].name = ""),
    (v) => {
      v.observedAt = 123;
      v.validUntil = 123;
    },
    (v) => {
      v.observedAt = "2026-10-14";
      v.validUntil = "2026-10-14";
    },
    (v) => {
      v.observedAt = "2026-02-30T00:00:00Z";
      v.validUntil = "2026-02-30T00:00:00Z";
    },
    (v) => (v.accounts[0].financialSummary.observation.state = ["AVAILABLE"]),
    (v) =>
      (v.accounts[0].financialSummary.observation.restriction = [
        "rate_limit_reached",
      ]),
  ]) {
    const invalid = globalThis.structuredClone(value);
    mutate(invalid);
    assert.throws(() => validateFinancialOverview(invalid));
  }
  const unicodeName = globalThis.structuredClone(value);
  unicodeName.accounts[0].name = "😀".repeat(160);
  unicodeName.accounts[0].ref = "a".repeat(128);
  assert.doesNotThrow(() => validateFinancialOverview(unicodeName));
  unicodeName.accounts[0].name += "😀";
  assert.throws(() => validateFinancialOverview(unicodeName));
});
