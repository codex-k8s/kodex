import { describe, expect, it } from "vitest";

import {
  accountAllows,
  isPendingDeviceAuthorization,
  isRuntimeEligible,
  pageAllowsAccountCreation,
  readableProviderBlocker,
  safeVerificationUri,
  normalizeProviderAccountCandidates,
  toggleProviderAccountCandidate,
  upsertProviderAccount,
  type ProviderAccount,
} from "./model";

function account(overrides: Partial<ProviderAccount> = {}): ProviderAccount {
  return {
    ref: "pacc_primary",
    version: 1,
    definitionKey: "openai-codex",
    name: "Основная запись",
    externalAccountMasked: "ow***er",
    state: "AUTHORIZED",
    enabled: true,
    ready: true,
    nextActions: ["DISABLE", "REVOKE"],
    createdAt: "2026-08-30T08:00:00Z",
    updatedAt: "2026-08-30T08:00:00Z",
    ...overrides,
  };
}

describe("provider account model", () => {
  it("разрешает действия только из server-owned nextActions", () => {
    const item = account();
    expect(accountAllows(item, "DISABLE")).toBe(true);
    expect(accountAllows(item, "ENABLE")).toBe(false);
    expect(pageAllowsAccountCreation(["CREATE_CONNECTION"])).toBe(true);
    expect(pageAllowsAccountCreation(["CONFIGURE_CREDENTIAL"])).toBe(false);
  });

  it("считает runtime eligible только готовую авторизованную запись", () => {
    expect(isRuntimeEligible(account())).toBe(true);
    expect(isRuntimeEligible(account({ ready: false }))).toBe(false);
    expect(isRuntimeEligible(account({ state: "DISABLED" }))).toBe(false);
  });

  it("принимает только HTTPS verification URI и ограничивает blocker copy", () => {
    expect(safeVerificationUri("https://provider.example/device")).toBe(
      "https://provider.example/device",
    );
    expect(safeVerificationUri("http://provider.example/device")).toBeNull();
    expect(safeVerificationUri("javascript:alert(1)")).toBeNull();
    expect(readableProviderBlocker("AUTH_MATERIAL_UNAVAILABLE")).toBe(
      "AUTHORIZATION_UNAVAILABLE",
    );
    expect(readableProviderBlocker("INTERNAL_DETAIL_42")).toBe("UNKNOWN");
  });

  it("останавливает просроченный device flow и обновляет account без дублей", () => {
    const pending = account({
      state: "PENDING_AUTHORIZATION",
      authorization: {
        ref: "pauth_one",
        method: "DEVICE_CODE",
        state: "PENDING",
        expiresAt: "2026-08-30T08:01:00Z",
      },
    });
    expect(
      isPendingDeviceAuthorization(pending, Date.parse("2026-08-30T08:00:00Z")),
    ).toBe(true);
    expect(
      isPendingDeviceAuthorization(pending, Date.parse("2026-08-30T08:02:00Z")),
    ).toBe(false);
    expect(
      upsertProviderAccount([pending], { ...pending, version: 2 }),
    ).toEqual([expect.objectContaining({ ref: "pacc_primary", version: 2 })]);
  });

  it("выбирает одну или несколько учётных записей согласно policy", () => {
    const first = { accountRef: "pacc_first", weight: 7 };
    expect(
      toggleProviderAccountCandidate([], first.accountRef, "FIXED"),
    ).toEqual([{ accountRef: first.accountRef, weight: 1 }]);
    expect(
      toggleProviderAccountCandidate([first], "pacc_second", "FIXED"),
    ).toEqual([{ accountRef: "pacc_second", weight: 1 }]);
    expect(
      toggleProviderAccountCandidate([first], "pacc_second", "WEIGHTED"),
    ).toEqual([first, { accountRef: "pacc_second", weight: 1 }]);
    expect(
      toggleProviderAccountCandidate([first], first.accountRef, "FIXED"),
    ).toEqual([]);
  });

  it("нормализует веса при смене policy без мутации входа", () => {
    const input = [
      { accountRef: "pacc_first", weight: 7 },
      { accountRef: "pacc_second", weight: 3 },
    ];
    expect(normalizeProviderAccountCandidates(input, "FIXED")).toEqual([
      { accountRef: "pacc_first", weight: 1 },
    ]);
    expect(normalizeProviderAccountCandidates(input, "LEAST_USED")).toEqual([
      { accountRef: "pacc_first", weight: 1 },
      { accountRef: "pacc_second", weight: 1 },
    ]);
    expect(input[0]?.weight).toBe(7);
  });
});
