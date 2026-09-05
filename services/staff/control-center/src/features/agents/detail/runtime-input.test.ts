import { describe, expect, it } from "vitest";
import { catalogStatusFixture } from "@/test-utils/runtime-catalog-fixture";
import type {
  AccountModelSnapshot,
  ModelSelection,
} from "@/features/providers/model-catalog";
import {
  pinnedRuntimeInput,
  runtimeCandidates,
  type RuntimeForm,
} from "./runtime-input";
function account(accountRef: string, digest: string): AccountModelSnapshot {
  return {
    accountRef,
    providerDefinitionKey: "openai-codex",
    catalogDigest: digest,
    catalogRevision: `mcat_${digest}`,
    catalogStatus: catalogStatusFixture,
    model: {
      id: "model",
      providerDefinitionKey: "openai-codex",
      available: true,
      eligibleProviderAccountRefs: [accountRef],
      reasoningEfforts: ["low", "high"],
      defaultReasoningEffort: "high",
      readinessBlockers: [],
    },
  };
}
function selection(): ModelSelection {
  return {
    model: "model",
    providerDefinitionKey: "openai-codex",
    accounts: [
      account("first", "a".repeat(64)),
      account("second", "b".repeat(64)),
    ],
  };
}
const form: RuntimeForm = {
  runtimeProfileRef: "profile",
  model: "model",
  providerPolicyMode: "WEIGHTED",
  providerAccounts: [
    { accountRef: "second", weight: 3 },
    { accountRef: "first", weight: 1 },
  ],
};
describe("Pinned runtime mutation input", () => {
  it("связывает каждый account с собственным каталогом и не передаёт readback default", () => {
    const current = form.providerAccounts.map((value) => ({
      ...value,
      defaultReasoningEffort: "high",
    }));
    expect(runtimeCandidates(current)).toEqual(form.providerAccounts);
    const input = pinnedRuntimeInput(
      { ...form, providerAccounts: current },
      selection(),
      "openai-codex",
    );
    expect(input.providerAccounts).toEqual([
      {
        accountRef: "second",
        weight: 3,
        catalogRevision: `mcat_${"b".repeat(64)}`,
        catalogDigest: "b".repeat(64),
        providerDefinitionKey: "openai-codex",
      },
      {
        accountRef: "first",
        weight: 1,
        catalogRevision: `mcat_${"a".repeat(64)}`,
        catalogDigest: "a".repeat(64),
        providerDefinitionKey: "openai-codex",
      },
    ]);
    expect(JSON.stringify(input)).not.toContain("defaultReasoningEffort");
  });
  it("не публикует результат lookup прежней модели или account", () => {
    expect(() =>
      pinnedRuntimeInput(
        { ...form, model: "changed" },
        selection(),
        "openai-codex",
      ),
    ).toThrow();
    expect(() => pinnedRuntimeInput(form, undefined, "openai-codex")).toThrow();
    expect(() =>
      pinnedRuntimeInput(
        {
          ...form,
          providerAccounts: [
            { accountRef: "foreign", weight: 1 },
            { accountRef: "first", weight: 1 },
          ],
        },
        selection(),
        "openai-codex",
      ),
    ).toThrow();
  });
  it.each(["PENDING", "FAILED", "EXPIRED"] as const)(
    "не публикует %s каталог",
    (state) => {
      const value = selection();
      const first = value.accounts[0];
      if (!first) throw new Error("Synthetic account is missing");
      first.catalogStatus = { state };
      expect(() => pinnedRuntimeInput(form, value, "openai-codex")).toThrow();
    },
  );
  it("повторно проверяет expiry перед отправкой, даже если dropdown был READY", () => {
    const value = selection();
    const first = value.accounts[0];
    if (!first) throw new Error("Synthetic account is missing");
    first.catalogStatus = {
      ...catalogStatusFixture,
      expiresAt: "2000-01-01T00:00:00Z",
    };
    expect(() => pinnedRuntimeInput(form, value, "openai-codex")).toThrow();
  });
});
