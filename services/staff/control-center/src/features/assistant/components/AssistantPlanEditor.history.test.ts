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
  getAgent: vi.fn(() => new Promise(() => {})),
  listPlatformCapabilities: vi.fn(() => new Promise(() => {})),
}));
vi.mock("@/shared/api/client", () => ({
  requestSignal: (signal?: AbortSignal) =>
    signal ?? new AbortController().signal,
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
  return renderPlan(plan(state), readonly);
}

async function renderPlan(input: AssistantPlan, readonly = false) {
  const before = JSON.stringify(input);
  const events: unknown[] = [];
  const app = createSSRApp({
    render: () =>
      h(Editor, {
        plan: input,
        readonly,
        onSave: (...args) => events.push(args),
        onValidate: () => events.push("validate"),
        onApply: () => events.push("apply"),
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

function capabilityPlan(
  state: AssistantPlan["state"],
  enabled = true,
): AssistantPlan {
  const input = plan(state);
  input.operations = [
    {
      ref: "op_capability_synthetic",
      type: "CHANGE_CAPABILITY",
      action: "UPDATE",
      title:
        "CHANGE_CAPABILITY platform.artifact.manage для agt_synthetic с expectedVersion=17",
      summary:
        "Сотрудник сможет изменять файлы проекта.\nДополнительное последствие: потребуется проверка результата <script>не выполнять</script>.",
      target: {
        kind: "AGENT",
        name: "Аналитик",
        ref: "agt_synthetic",
        version: 17,
      },
      expectedVersion: 17,
      parameters: {
        agentRef: "agt_synthetic",
        capabilityKey: "platform.artifact.manage",
        enabled,
      },
      before: { enabled: !enabled },
      after: { enabled },
      selected: true,
      permitted: true,
      validationProblems: [],
    },
  ];
  return input;
}

describe("AssistantPlanEditor: понятное отображение платформенного права", () => {
  it.each([
    ["APPLIED", false, true, "ru"],
    ["APPLIED", false, false, "ru"],
    ["DRAFT", true, true, "ru"],
    ["DRAFT", false, true, "ru"],
    ["APPLIED", false, true, "en"],
  ] as const)(
    "%s readonly=%s enabled=%s locale=%s сохраняет последствия и original envelope",
    async (state, readonly, enabled, locale) => {
      i18n.global.locale.value = locale;
      const input = capabilityPlan(state, enabled);
      const item = input.operations[0];
      if (!item) throw new Error("Synthetic capability operation is missing");
      const html = await renderPlan(input, readonly);
      const heading = html.match(
        /<span[^>]*class="assistant-plan-operation__title"[^>]*>([^]*?)<\/span>/,
      )?.[1];
      expect(heading).toContain(
        "Аналитик · " +
          i18n.global.t(
            "assistant.planEditor.capabilities.platform_artifact_manage",
          ),
      );
      expect(heading).not.toContain("platform.artifact.manage");
      expect(heading).not.toContain("agt_synthetic");
      const presentation = html.match(
        /<div[^>]*class="assistant-capability-presentation"[^>]*>([^]*?)<\/div>/,
      )?.[1];
      expect(presentation).not.toContain("display:none");
      expect(presentation).toContain(
        i18n.global.t(
          enabled
            ? "assistant.planEditor.grantEnableShort"
            : "assistant.planEditor.grantDisableShort",
        ),
      );
      expect(presentation).toContain(
        i18n.global.t("serverMessages.CAPABILITY_ARTIFACT_MANAGE_DESCRIPTION"),
      );
      expect(presentation).toContain("Дополнительное последствие");
      expect(presentation).toContain(
        "&lt;script&gt;не выполнять&lt;/script&gt;",
      );
      expect(presentation).toContain("\n");
      const details = html.slice(
        html.indexOf('<details class="assistant-plan-friendly__snapshot"'),
      );
      expect(details).toContain(item.title);
      expect(details).toContain("agentRef");
      expect(details).toContain("agt_synthetic");
      expect(details).toContain("platform.artifact.manage");
      expect(details).not.toMatch(/<details[^>]*\sopen(?:\s|=|>)/);
    },
  );

  it.each(["platform.unknown.manage", "constructor"])(
    "не угадывает заголовок неизвестного права %s",
    async (key) => {
      const input = capabilityPlan("APPLIED");
      const item = input.operations[0];
      if (!item) throw new Error("Synthetic capability operation is missing");
      item.parameters.capabilityKey = key;
      const html = await renderPlan(input);
      expect(html).not.toContain('class="assistant-capability-presentation"');
      expect(html).toContain(item.title);
    },
  );

  it("не выдумывает имя получателя или направление из original title", async () => {
    const input = capabilityPlan("APPLIED");
    const item = input.operations[0];
    if (!item) throw new Error("Synthetic capability operation is missing");
    item.target.name = "";
    expect(await renderPlan(input)).not.toContain(
      'class="assistant-capability-presentation"',
    );
    item.target.name = "Аналитик";
    delete item.parameters.enabled;
    expect(await renderPlan(input)).not.toContain(
      'class="assistant-capability-presentation"',
    );
  });
});
