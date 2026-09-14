import assert from "node:assert/strict";
import test from "node:test";
import {
  selectWorkspaceProviderAccount,
  workspaceModelQuery,
  workspaceProviderAccountCandidates,
  workspaceProviderAccountRef,
} from "./runtime-provider-catalog.mjs";

const model = "gpt-5.6-sol";
const account = "pacc_runtime_fixture";
const item = {
  id: model,
  available: true,
  eligibleProviderAccountRefs: ["pacc_another", account],
};

test("точная account задаётся до чтения model catalog без замены модели", () => {
  const query = workspaceModelQuery(model, account);
  assert.equal(query.get("providerAccountRef"), account);
  assert.equal(query.get("query"), model);
  assert.equal(selectWorkspaceProviderAccount([item], model, account), account);
});

test("без явной account сохраняется выбор из общего каталога", () => {
  assert.equal(workspaceModelQuery(model).has("providerAccountRef"), false);
  assert.equal(selectWorkspaceProviderAccount([item], model), "pacc_another");
});

test("кандидаты для account-specific каталога имеют устойчивый порядок", () => {
  const candidates = workspaceProviderAccountCandidates([
    { ref: account, state: "AUTHORIZED", enabled: true, ready: true },
    {
      ref: "pacc_disabled",
      state: "AUTHORIZED",
      enabled: false,
      ready: true,
    },
    {
      ref: "pacc_another",
      state: "AUTHORIZED",
      enabled: true,
      ready: true,
    },
    { ref: account, state: "AUTHORIZED", enabled: true, ready: true },
  ]);
  assert.deepEqual(candidates, ["pacc_another", account]);
  assert.deepEqual(
    workspaceProviderAccountCandidates(
      [{ ref: account, state: "AUTHORIZED", enabled: true, ready: true }],
      account,
    ),
    [account],
  );
  assert.deepEqual(
    workspaceProviderAccountCandidates([], "pacc_unavailable"),
    [],
  );
});

test("недоступная account не подменяется другой eligible account", () => {
  assert.throws(() =>
    selectWorkspaceProviderAccount([item], model, "pacc_unavailable"),
  );
  assert.throws(() =>
    selectWorkspaceProviderAccount(
      [{ ...item, available: false }],
      model,
      account,
    ),
  );
});

test("другая модель и пустой каталог закрыто отклоняются", () => {
  assert.throws(() => selectWorkspaceProviderAccount([item], "other", account));
  assert.throws(() => selectWorkspaceProviderAccount([], model, account));
  assert.throws(() =>
    selectWorkspaceProviderAccount(undefined, model, account),
  );
});

test("невалидный ref отклоняется до запроса и не попадает в сообщение", () => {
  for (const value of [
    " ",
    "pacc_a/foreign",
    "pacc_",
    "pacc_" + "a".repeat(97),
    null,
  ])
    assert.throws(() => workspaceProviderAccountRef(value), {
      message: "Runtime provider account reference is invalid",
    });
});
