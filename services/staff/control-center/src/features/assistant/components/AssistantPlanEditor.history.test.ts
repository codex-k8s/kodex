import { renderToString } from "@vue/server-renderer";
import { createSSRApp, defineComponent, h } from "vue";
import { afterEach, describe, expect, it, vi } from "vitest";

vi.mock("@/shared/locale", () => ({ currentLocale: () => "ru" }));
vi.mock("@/features/platform/store", () => ({
  usePlatformStore: () => ({
    bootstrap: { organizationRef: "org_synthetic" },
    runtimes: {},
    projects: {},
    problems: {},
  }),
}));
vi.mock("@/features/runtime/store", () => ({ useRuntimeStore: () => ({}) }));
vi.mock("@/shared/api/generated/openapi/sdk.gen", () => ({
  listAgents: vi.fn(() => new Promise(() => {})),
}));
vi.mock("@/features/role-images/api", () => ({
  loadRoleEnvironmentCatalog: vi.fn(() => new Promise(() => {})),
}));
vi.mock("@/features/role-images/RoleImageDockerfileEditor.vue", () => ({
  default: defineComponent({ render: () => h("span") }),
}));

import { i18n } from "@/app/i18n";
import type { AssistantPlan } from "@/shared/api/generated/openapi/types.gen";
import Editor from "./AssistantPlanEditor.vue";

afterEach(() => {
  i18n.global.locale.value = "ru";
});

function plan(state: AssistantPlan["state"]): AssistantPlan {
  const parameters = {
    agentRef: "agt_synthetic",
    name: "Проверенный образ",
    environmentKey: "standard",
    dockerfile: "FROM example.invalid/synthetic@sha256:" + "a".repeat(64),
  };
  return {
    ref: "pln_synthetic",
    version: 3,
    revision: 2,
    state,
    conversationRef: "cnv_synthetic",
    projectRef: "prj_synthetic",
    applied: state === "APPLIED",
    auditSummary: "Создать рецепт",
    contentDigest: "a".repeat(64),
    validationProblems: [],
    nextActions: [],
    operations: [
      {
        ref: "op_synthetic",
        type: "CREATE_ROLE_IMAGE_RECIPE",
        action: "CREATE",
        title: "Создать рецепт",
        summary: "Подготовить образ",
        target: { kind: "ROLE_IMAGE_RECIPE", name: parameters.name },
        parameters,
        before: {},
        after: { ...parameters },
        selected: true,
        permitted: true,
        validationProblems: [],
      },
    ],
  };
}

async function render(state: AssistantPlan["state"], readonly = false) {
  const input = plan(state);
  const before = JSON.stringify(input);
  const events: unknown[] = [];
  const app = createSSRApp({
    render: () =>
      h(Editor, {
        plan: input,
        readonly,
        onSave: (...args) => events.push(args),
      }),
  });
  app.use(i18n);
  const html = await renderToString(app);
  expect(JSON.stringify(input)).toBe(before);
  expect(events).toEqual([]);
  return html;
}

describe("AssistantPlanEditor: история рецепта образа", () => {
  it.each(["ru", "en"] as const)(
    "APPLIED %s не показывает ошибки и подсказки editable каталога до его загрузки",
    async (locale) => {
      i18n.global.locale.value = locale;
      const html = await render("APPLIED");
      expect(html).not.toContain(
        i18n.global.t("assistant.planEditor.roleImageAgentUnavailable"),
      );
      expect(html).not.toContain(
        i18n.global.t("assistant.planEditor.roleImageAgentFixed"),
      );
      expect(html).not.toContain('name="assistant-role-image-environment-0"');
      expect(html).not.toMatch(/<option[^>]*value="standard"/);
      expect(html).toContain("Проверенный образ");
      expect(html).toContain("agt_synthetic");
      expect(html).toContain("standard");
    },
  );

  it("readonly DRAFT также не подменяет недоступное имя техническим ключом", async () => {
    const html = await render("DRAFT", true);
    expect(html).not.toContain(
      i18n.global.t("assistant.planEditor.roleImageAgentUnavailable"),
    );
    expect(html).not.toContain(
      i18n.global.t("assistant.planEditor.roleImageAgentFixed"),
    );
    expect(html).not.toContain('name="assistant-role-image-environment-0"');
    expect(html).toContain("agt_synthetic");
    expect(html).toContain("standard");
  });

  it("editable DRAFT сохраняет проверку каталога, исходный выбор и ограниченную привязку", async () => {
    const html = await render("DRAFT");
    expect(html).toContain(
      i18n.global.t("assistant.planEditor.roleImageAgentUnavailable"),
    );
    expect(html).toContain(
      i18n.global.t("assistant.planEditor.roleImageAgentFixed"),
    );
    expect(html).toContain('name="assistant-role-image-environment-0"');
    expect(html).toMatch(/<option[^>]*value="standard"/);
  });
});
