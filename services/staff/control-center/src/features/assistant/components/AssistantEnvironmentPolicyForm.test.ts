import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";
import type { ComputedRef } from "vue";
import type { EditablePlanOperation } from "@/features/assistant/model";
import { defaultRuntimeEnvironmentPolicy } from "@/features/runtime/environment-form";
import type {
  AssistantPlanOperation,
  RuntimeEnvironmentPolicyInput,
} from "@/shared/api/generated/openapi/types.gen";
import { captureSetupState } from "@/test-utils/setup-harness";
import Component from "./AssistantEnvironmentPolicyForm.vue";

async function state(
  proposed?: unknown,
  before: unknown = defaultRuntimeEnvironmentPolicy(),
  type: AssistantPlanOperation["type"] = "PREPARE_RUNTIME_ENVIRONMENT_REVISION",
) {
  const operation: EditablePlanOperation = {
    parametersText: JSON.stringify(
      proposed === undefined ? {} : { policy: proposed },
    ),
    beforeText: JSON.stringify({ policyInput: before }),
    afterText: "{}",
    value: {
      ref: "op_synthetic",
      type,
      action: "UPDATE",
      title: "Окружение",
      summary: "Настройки окружения",
      target: { kind: "ENVIRONMENT", ref: "env_synthetic", name: "Окружение" },
      parameters: {},
      before: { policyInput: before },
      after: {},
      selected: true,
      permitted: true,
      validationProblems: [],
    },
  };
  return (await captureSetupState(Component, undefined, {
    operation,
    disabled: false,
  })) as unknown as {
    compact: ComputedRef<boolean>;
    policy: ComputedRef<RuntimeEnvironmentPolicyInput>;
    problems: ComputedRef<string[]>;
  };
}

describe("Компактная политика плана окружения", () => {
  it("сворачивает неизменённую проверенную политику при смене только образа", async () => {
    const value = await state();
    expect(value.compact.value).toBe(true);
    expect(value.problems.value).toEqual([]);
    expect(value.policy.value).toEqual(defaultRuntimeEnvironmentPolicy());
  });
  it("точно тот же policy payload не объявляется изменением доступа", async () => {
    expect((await state(defaultRuntimeEnvironmentPolicy())).compact.value).toBe(
      true,
    );
  });
  it.each(["resources", "volumes", "network", "web"])(
    "изменение %s остаётся развёрнутым",
    async (kind) => {
      const policy = defaultRuntimeEnvironmentPolicy();
      if (kind === "resources") policy.resources.cpuRequestMilli = 1000;
      if (kind === "volumes")
        policy.volumes = [
          { name: "cache", kind: "EPHEMERAL_DISK", sizeMib: 256 },
        ];
      if (kind === "network")
        policy.networkDestinations = ["DNS", "DNS", "PROVIDER_PROXY"];
      if (kind === "web") policy.webAccess = { mode: "FULL_PUBLIC", rules: [] };
      expect((await state(policy)).compact.value).toBe(false);
    },
  );
  it("сохраняет видимость действующего доступа к интернету даже без изменения policy", async () => {
    const before = defaultRuntimeEnvironmentPolicy();
    before.webAccess = { mode: "FULL_PUBLIC", rules: [] };
    expect((await state(undefined, before)).compact.value).toBe(false);
  });
  it.each([
    undefined,
    {},
    { ...defaultRuntimeEnvironmentPolicy(), kubernetesAccess: "NATIVE" },
  ])(
    "не скрывает неизвестную или недопустимую исходную policy %o",
    async (before) => {
      const value = await state(
        undefined,
        before === undefined ? null : before,
      );
      expect(value.compact.value).toBe(false);
      expect(value.problems.value).not.toEqual([]);
    },
  );
  it("invalid ресурсы не исчезают внутри закрытого details", async () => {
    const before = defaultRuntimeEnvironmentPolicy();
    before.resources.cpuLimitMilli = 0;
    const value = await state(undefined, before);
    expect(value.compact.value).toBe(false);
    expect(value.problems.value.length).toBeGreaterThan(0);
  });
  it("создание нового окружения сохраняет прежнюю полную форму", async () => {
    expect(
      (await state(undefined, {}, "CREATE_RUNTIME_ENVIRONMENT_DRAFT")).compact
        .value,
    ).toBe(false);
  });
  it("применяет только presentation и оставляет validation/dirty/error снаружи details", () => {
    const source = readFileSync(
      new URL("./AssistantEnvironmentPolicyForm.vue", import.meta.url),
      "utf8",
    );
    expect(source).toContain(`:is="compact ? 'details' : 'div'"`);
    expect(source).toContain("assistant.planEditor.environmentPolicyReview");
    expect(source).toContain("assistant.planEditor.environmentPolicySummary");
    expect(source).toContain('emit("parameter", "policy", value)');
    expect(source).toContain('emit("dirty")');
    expect(source).toContain('emit("valid", valid)');
    expect(source.indexOf('v-for="problem in problems"')).toBeGreaterThan(
      source.indexOf("</component>"),
    );
    expect(source).not.toContain('v-if="compact"\n      :policy=');
  });
});
