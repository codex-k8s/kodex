import assert from "node:assert/strict";
import { test } from "node:test";
import {
  financialSummary,
  financialCredits,
  financialReset,
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
