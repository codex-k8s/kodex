import { readFileSync } from "node:fs";
import { describe, expect, it, vi } from "vitest";
import { createSSRApp, type ComputedRef } from "vue";
import { renderToString } from "@vue/server-renderer";
vi.mock("@/shared/locale", () => ({ currentLocale: () => "ru" }));
import { i18n } from "@/app/i18n";
import type { EditablePlanOperation } from "@/features/assistant/model";
import { defaultRuntimeEnvironmentPolicy } from "@/features/runtime/environment-form";
import type {
  AssistantPlanOperation,
  RuntimeEnvironmentPolicyInput,
} from "@/shared/api/generated/openapi/types.gen";
import { captureSetupState } from "@/test-utils/setup-harness";
import Component from "./AssistantEnvironmentPolicyForm.vue";

function planOperation(
  proposed?: unknown,
  before: unknown = defaultRuntimeEnvironmentPolicy(),
  type: AssistantPlanOperation["type"] = "PREPARE_RUNTIME_ENVIRONMENT_REVISION",
): EditablePlanOperation {
  return {
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
}

async function state(
  proposed?: unknown,
  before: unknown = defaultRuntimeEnvironmentPolicy(),
  type: AssistantPlanOperation["type"] = "PREPARE_RUNTIME_ENVIRONMENT_REVISION",
) {
  return (await captureSetupState(Component, undefined, {
    operation: planOperation(proposed, before, type),
    disabled: false,
  })) as unknown as {
    compact: ComputedRef<boolean>;
    policy: ComputedRef<RuntimeEnvironmentPolicyInput>;
    problems: ComputedRef<string[]>;
  };
}

function readOnlyPolicy(): RuntimeEnvironmentPolicyInput {
  const policy = defaultRuntimeEnvironmentPolicy();
  policy.webAccess = {
    mode: "ALLOWLIST_READ_ONLY",
    rules: Array.from({ length: 10 }, (_, index) => ({
      domainPattern: `docs${String(index)}.example.com`,
      protocol: "HTTPS",
      port: 443,
      httpMethods: ["GET", "HEAD", "OPTIONS"],
    })),
  };
  return policy;
}

describe("Компактная политика плана окружения", () => {
  it("сворачивает неизменённую проверенную политику с десятью read-only правилами", async () => {
    const before = readOnlyPolicy();
    const value = await state(undefined, before);
    expect(value.compact.value).toBe(true);
    expect(value.problems.value).toEqual([]);
    expect(value.policy.value).toEqual(before);
    expect((await state(structuredClone(before), before)).compact.value).toBe(
      true,
    );
  });
  it.each(["host", "methods", "count", "order", "resources", "volumes"])(
    "изменение %s read-only политики не скрывается",
    async (kind) => {
      const before = readOnlyPolicy();
      const proposed = structuredClone(before);
      const [first] = proposed.webAccess.rules;
      if (!first) throw new Error("Policy fixture has no rules");
      if (kind === "host") first.domainPattern = "other.example.com";
      if (kind === "methods") first.httpMethods = ["GET"];
      if (kind === "count") proposed.webAccess.rules.pop();
      if (kind === "order") proposed.webAccess.rules.reverse();
      if (kind === "resources") proposed.resources.cpuRequestMilli = 1000;
      if (kind === "volumes")
        proposed.volumes = [
          { name: "cache", kind: "EPHEMERAL_DISK", sizeMib: 256 },
        ];
      expect((await state(proposed, before)).compact.value).toBe(false);
    },
  );
  it.each(["empty", "domain", "duplicate", "write-method"])(
    "invalid %s read-only политика остаётся видимой вместе с ошибкой",
    async (kind) => {
      const before = readOnlyPolicy();
      const [first, second] = before.webAccess.rules;
      if (!first || !second)
        throw new Error("Policy fixture has too few rules");
      if (kind === "empty") before.webAccess.rules = [];
      if (kind === "domain") first.domainPattern = "*";
      if (kind === "duplicate") second.domainPattern = first.domainPattern;
      if (kind === "write-method") first.httpMethods = ["POST"];
      const value = await state(undefined, before);
      expect(value.compact.value).toBe(false);
      expect(value.problems.value.length).toBeGreaterThan(0);
    },
  );
  it("не сворачивает read-only политику без проверенной исходной или при создании нового окружения", async () => {
    expect((await state(readOnlyPolicy(), {})).compact.value).toBe(false);
    expect(
      (
        await state(
          readOnlyPolicy(),
          readOnlyPolicy(),
          "CREATE_RUNTIME_ENVIRONMENT_DRAFT",
        )
      ).compact.value,
    ).toBe(false);
  });
  it("оставляет неизменённый ALLOWLIST_FULL доступ полностью видимым", async () => {
    const before = readOnlyPolicy();
    before.webAccess.mode = "ALLOWLIST_FULL";
    expect((await state(undefined, before)).compact.value).toBe(false);
  });
  it.each(["ru", "en"] as const)(
    "показывает режим и число правил в %s, сохраняя поля внутри закрытого details",
    async (locale) => {
      i18n.global.locale.value = locale;
      try {
        const app = createSSRApp(Component, {
          operation: planOperation(undefined, readOnlyPolicy()),
          disabled: false,
        });
        app.use(i18n);
        const html = await renderToString(app);
        const summary = html.match(/<summary[^>]*>([\s\S]*?)<\/summary>/)?.[1];
        expect(summary).toContain(
          i18n.global.t("runtime.webAccessModeLabel.ALLOWLIST_READ_ONLY"),
        );
        expect(summary).toContain(locale === "ru" ? "правил: 10" : "rules: 10");
        expect(html).toContain("<details");
        expect(html).not.toMatch(/<details[^>]*\sopen(?:\s|>)/);
        expect(html).toContain('name="runtime-web-domain-0"');
        expect(html).toContain("docs0.example.com");
        expect(html).toContain('name="runtime-web-domain-9"');
      } finally {
        i18n.global.locale.value = "ru";
      }
    },
  );
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
