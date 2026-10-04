import { describe, expect, it } from "vitest";
import {
  assistantRuntimePlanOwner,
  assistantRuntimeReasoning,
} from "./runtime-configuration-plan";
import type { ModelSelection } from "@/features/providers/model-catalog";
import { catalogStatusFixture } from "@/test-utils/runtime-catalog-fixture";
import type { AssistantPlanOperationInput } from "@/shared/api/generated/openapi/types.gen";
import {
  editableOperations,
  operationInputs,
  updateOperationParameter,
} from "./model";

function selection(efforts: string[][]): ModelSelection {
  return {
    model: "model-current",
    providerDefinitionKey: "openai-codex",
    accounts: efforts.map((reasoningEfforts, index) => ({
      accountRef: `pacc_${String(index)}`,
      providerDefinitionKey: "openai-codex",
      catalogRevision: `mcat_${"a".repeat(64)}`,
      catalogDigest: "a".repeat(64),
      catalogStatus: { ...catalogStatusFixture },
      model: {
        id: "model-current",
        providerDefinitionKey: "openai-codex",
        available: true,
        eligibleProviderAccountRefs: [`pacc_${String(index)}`],
        readinessBlockers: [],
        reasoningEfforts,
        defaultReasoningEffort: reasoningEfforts[0] ?? "",
      },
    })),
  };
}
describe("assistant runtime reasoning", () => {
  const check = (input: ModelSelection | undefined, refs = ["pacc_0"]) =>
    assistantRuntimeReasoning(input, "model-current", "openai-codex", refs);
  it("отличает подтверждённую модель без рассуждений от незагруженного каталога", () => {
    expect(check(undefined)).toBeUndefined();
    expect(check(selection([]))).toBeUndefined();
    expect(check(selection([[]]))).toEqual({ efforts: [], unsupported: true });
    expect(check(selection([[], []]), ["pacc_0", "pacc_1"])).toEqual({
      efforts: [],
      unsupported: true,
    });
  });
  it("показывает только пересечение усилий всех выбранных аккаунтов", () => {
    expect(
      check(
        selection([
          ["high", "low", "medium"],
          ["high", "medium"],
        ]),
        ["pacc_1", "pacc_0"],
      ),
    ).toEqual({ efforts: ["medium", "high"], unsupported: false });
    expect(check(selection([["low"], ["high"]]), ["pacc_0", "pacc_1"])).toEqual(
      { efforts: [], unsupported: false },
    );
    expect(check(selection([["low"], []]), ["pacc_0", "pacc_1"])).toEqual({
      efforts: [],
      unsupported: false,
    });
  });
  it("закрыто отвергает позднюю выборку другого аккаунта, модели, провайдера и истёкший каталог", () => {
    const input = selection([["low"]]);
    expect(check(input, ["pacc_new"])).toBeUndefined();
    expect(check(input, ["pacc_0", "pacc_1"])).toBeUndefined();
    expect(check(input, ["pacc_0", "pacc_0"])).toBeUndefined();
    expect(check({ ...input, model: "model-old" })).toBeUndefined();
    expect(
      check({ ...input, providerDefinitionKey: "foreign" }),
    ).toBeUndefined();
    const expired = selection([[]]);
    const snapshot = expired.accounts[0];
    if (!snapshot) throw new Error("Missing model snapshot");
    snapshot.catalogStatus.expiresAt = "2000-01-01T00:00:00Z";
    expect(check(expired)).toBeUndefined();
    snapshot.catalogStatus = { ...catalogStatusFixture, state: "PENDING" };
    expect(check(expired)).toBeUndefined();
  });
});

function operation(
  scope: "SYSTEM" | "PROJECT" = "SYSTEM",
): AssistantPlanOperationInput {
  const owner = {
    agentRef: "agt_assistant",
    organizationRef: "org_synthetic",
    assistantScope: scope,
    scopeKind: scope === "SYSTEM" ? "ORGANIZATION" : "PROJECT",
    ...(scope === "PROJECT"
      ? { projectRef: "prj_synthetic", assistantProfileRef: "asstp_synthetic" }
      : { projectRef: "", assistantProfileRef: "" }),
  };
  const runtimeProfilePin = {
    ref: "runtime_synthetic",
    version: 7,
    runtimeRevision: "runtime-revision",
  };
  return {
    ref: "operation_runtime",
    type: "PREPARE_ASSISTANT_RUNTIME_CONFIGURATION",
    action: "UPDATE",
    title: "Модель",
    summary: "Настроить модель",
    selected: true,
    permitted: true,
    validationProblems: [],
    target: { kind: "AGENT", ref: "agt_assistant", name: "Kodex", version: 3 },
    expectedVersion: 3,
    parameters: { ...owner, model: "gpt-6.1-sol", runtimeProfilePin },
    before: { ...owner, agentVersion: 3, runtimeProfilePin },
    after: { ...owner, model: "gpt-6.1-sol", runtimeProfilePin },
  };
}
describe("assistant runtime plan owner", () => {
  it.each(["SYSTEM", "PROJECT"] as const)(
    "сохраняет readonly profile pin и owner/version %s при правке модели и аккаунтов",
    (scope) => {
      const original = operation(scope);
      const [editable] = editableOperations([original]);
      if (!editable) throw new Error("Missing editable operation");
      updateOperationParameter(editable, "model", "gpt-6.1-sol-new");
      updateOperationParameter(editable, "providerAccounts", [
        { accountRef: "pacc_fresh", weight: 1 },
      ]);
      const [saved] = operationInputs([editable]);
      if (!saved) throw new Error("Missing saved operation");
      expect(saved.parameters).toEqual({
        ...original.parameters,
        model: "gpt-6.1-sol-new",
        providerAccounts: [{ accountRef: "pacc_fresh", weight: 1 }],
      });
      expect(saved.after).toEqual({
        ...original.after,
        model: "gpt-6.1-sol-new",
        providerAccounts: [{ accountRef: "pacc_fresh", weight: 1 }],
      });
      expect(saved.before).toEqual(original.before);
      expect(saved.target).toEqual(original.target);
      expect(saved.expectedVersion).toBe(original.expectedVersion);
      expect(saved.parameters.runtimeProfilePin).toEqual(
        original.parameters.runtimeProfilePin,
      );
      expect(assistantRuntimePlanOwner(saved, "org_synthetic")).toBe(true);
    },
  );
  it.each(["SYSTEM", "PROJECT"] as const)(
    "разрешает только закреплённый %s owner",
    (scope) => {
      expect(assistantRuntimePlanOwner(operation(scope), "org_synthetic")).toBe(
        true,
      );
      expect(assistantRuntimePlanOwner(operation(scope), undefined)).toBe(
        false,
      );
      expect(assistantRuntimePlanOwner(operation(scope), "org_foreign")).toBe(
        false,
      );
    },
  );
  it("не принимает другой agent, версию, область или профиль", () => {
    const base = operation();
    expect(
      assistantRuntimePlanOwner(
        { ...base, target: { ...base.target, ref: "agt_foreign" } },
        "org_synthetic",
      ),
    ).toBe(false);
    expect(
      assistantRuntimePlanOwner(
        { ...base, expectedVersion: 4 },
        "org_synthetic",
      ),
    ).toBe(false);
    expect(
      assistantRuntimePlanOwner(
        { ...base, after: { ...base.after, organizationRef: "org_foreign" } },
        "org_synthetic",
      ),
    ).toBe(false);
    const project = operation("PROJECT");
    expect(
      assistantRuntimePlanOwner(
        { ...base, before: { ...base.before, agentVersion: 4 } },
        "org_synthetic",
      ),
    ).toBe(false);
    expect(
      assistantRuntimePlanOwner(
        { ...base, before: { ...base.before, organizationRef: "org_foreign" } },
        "org_synthetic",
      ),
    ).toBe(false);
    expect(
      assistantRuntimePlanOwner(
        {
          ...project,
          parameters: { ...project.parameters, assistantProfileRef: "" },
          after: { ...project.after, assistantProfileRef: "" },
        },
        "org_synthetic",
      ),
    ).toBe(false);
    expect(
      assistantRuntimePlanOwner(
        {
          ...base,
          parameters: { ...base.parameters, projectRef: "prj_other" },
          after: { ...base.after, projectRef: "prj_other" },
        },
        "org_synthetic",
      ),
    ).toBe(false);
  });
});
