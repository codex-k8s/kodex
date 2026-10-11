import { readFileSync } from "node:fs";

import { createSSRApp, defineComponent, h } from "vue";
import { renderToString } from "vue/server-renderer";
import { createI18n } from "vue-i18n";
import { describe, expect, it, vi } from "vitest";
vi.mock("@/shared/locale", () => ({ currentLocale: () => "ru" }));
import { i18n } from "@/app/i18n";
import type { Agent, Run } from "@/shared/api/generated/openapi/types.gen";
import RunWorkItem from "./RunWorkItem.vue";
import ProjectAgentList from "./ProjectAgentList.vue";

const source = readFileSync(
  new URL("./RunWorkItem.vue", import.meta.url),
  "utf8",
);

function fixture(
  options: Partial<Omit<Run, "source" | "target">> & {
    target?: Run["target"] & { type: "AGENT" | "WORKFLOW" };
  } = {},
): Run {
  return {
    ref: "run_child",
    version: 1,
    projectRef: "project_own",
    sessionRef: "session_child",
    rootRunRef: "run_root",
    parentRunRef: "run_root",
    target: {
      type: "AGENT",
      ref: "agent_architect",
      displayName: "Архитектор",
      version: 1,
    },
    title: "ANALYSIS_AND_ARCHITECTURE",
    titleSource: "SERVER_DEFAULT",
    activitySummary: "Работает",
    state: "RUNNING",
    source: "AGENT_DELEGATION",
    initiator: { ref: "user_owner", displayName: "Владелец" },
    attempt: 1,
    graphRevision: 1,
    lastEventSequence: 1,
    usage: {
      totalTokens: 0,
      inputTokens: 0,
      cachedInputTokens: 0,
      cacheWriteInputTokens: 0,
      outputTokens: 0,
      reasoningOutputTokens: 0,
      modelContextWindow: 0,
    },
    artifactRefs: [],
    gateRefs: [],
    createdAt: "2026-10-11T10:00:00Z",
    nextActions: [],
    ...options,
  };
}

async function render(run: Run, locale = "ru") {
  const app = createSSRApp(RunWorkItem, { run, preserveProject: true });
  app.use(
    createI18n({
      legacy: false,
      locale,
      messages: {
        ru: i18n.global.getLocaleMessage("ru"),
        en: i18n.global.getLocaleMessage("en"),
      },
    }),
  );
  app.component(
    "RouterLink",
    defineComponent({
      props: { to: { type: String, required: true } },
      setup:
        (props, { slots }) =>
        () =>
          h("a", { href: props.to }, slots.default?.()),
    }),
  );
  return renderToString(app);
}

describe("RunWorkItem", () => {
  function assistant(): Run {
    return {
      ...fixture(),
      title: "Команда системному помощнику",
      source: "SYSTEM_ASSISTANT",
      target: {
        type: "SYSTEM_ASSISTANT",
        ref: "assistant_own",
        displayName: "Помощник",
        version: 1,
      },
      assistantPin: {
        scope: "PROJECT",
        organizationRef: "organization_own",
        projectRef: "project_own",
        assistantRef: "assistant_own",
        profileRef: "profile_own",
        conversationRef: "conversation_own",
      },
    };
  }
  it.each([
    ["ru", "Команда помощнику проекта"],
    ["en", "Command to project assistant"],
  ])(
    "называет штатную команду по точному PROJECT pin в %s",
    async (locale, title) => {
      const value = assistant();
      const before = JSON.stringify(value);
      const html = await render(value, locale);
      expect(html).toContain(title);
      expect(html).not.toContain("Команда системному помощнику");
      expect(html).toContain("/projects/project_own/runs/run_child");
      expect(JSON.stringify(value)).toBe(before);
    },
  );
  it.each(["USER_EDITED", "AGENT_PROPOSED"] as const)(
    "не заменяет название команды %s",
    async (titleSource) => {
      expect(await render({ ...assistant(), titleSource })).toContain(
        "Команда системному помощнику",
      );
    },
  );
  it("не приписывает PROJECT-команду SYSTEM, отсутствующему или чужому pin", async () => {
    const value = assistant();
    const pin = value.assistantPin;
    if (!pin) throw new Error("Missing assistant fixture pin");
    for (const candidate of [
      { ...value, assistantPin: undefined },
      {
        ...value,
        assistantPin: {
          ...pin,
          scope: "SYSTEM" as const,
          profileRef: undefined,
        },
      },
      { ...value, assistantPin: { ...pin, projectRef: "project_other" } },
      { ...value, assistantPin: { ...pin, assistantRef: "assistant_other" } },
      { ...value, assistantPin: { ...pin, profileRef: undefined } },
    ]) {
      const html = await render(candidate);
      expect(html).not.toContain("Команда помощнику проекта");
      expect(html).toContain("Команда системному помощнику");
    }
  });

  it("обзор сотрудника не выдаёт имя runtime profile за назначенное ENV", async () => {
    const agent: Agent = {
      ref: "agent_own",
      version: 1,
      projectRef: "project_own",
      name: "Архитектор",
      purpose: "Проектирование",
      roleDescription: "Архитектура",
      state: "READY",
      enabled: true,
      system: false,
      runtimeRef: "runtime_own",
      runtimeName: "Базовая среда",
      runtimeReady: true,
      capabilities: [],
      integrations: [],
      knowledgeArtifactRefs: [],
      updatedAt: "2026-10-12T00:00:00Z",
      nextActions: [],
    };
    const app = createSSRApp(ProjectAgentList, { agents: [agent] });
    app.use(
      createI18n({
        legacy: false,
        locale: "ru",
        messages: { ru: i18n.global.getLocaleMessage("ru") },
      }),
    );
    app.component(
      "RouterLink",
      defineComponent({
        props: ["to"],
        setup:
          (props, { slots }) =>
          () =>
            h("a", { href: props.to }, slots.default?.()),
      }),
    );
    const html = await renderToString(app);
    expect(html).toContain("Архитектор");
    expect(html).toContain("Проектирование");
    expect(html).toContain("Готов");
    expect(html).toContain("/projects/project_own/agents/agent_own");
    expect(html).not.toContain("Базовая среда");
    expect(html).not.toContain("Среда не указана");
  });
  it.each([
    ["ru", "Анализ и архитектура", "Исполнитель"],
    ["en", "Analysis and architecture", "Executor"],
  ] as const)(
    "показывает человеческое серверное название и точного исполнителя в %s",
    async (locale, title, executor) => {
      const html = await render(fixture(), locale);
      expect(html).toContain(title);
      expect(html).not.toContain("ANALYSIS_AND_ARCHITECTURE");
      expect(html).toContain(executor);
      expect(html).toContain("Архитектор");
      expect(html).toContain("Владелец");
      expect(html).toContain("/projects/project_own/runs/run_child");
      for (const kind of [
        "SOFTWARE_CHANGE",
        "INTAKE",
        "ANALYSIS_AND_ARCHITECTURE",
        "IMPLEMENTATION",
        "DOCUMENTATION_REVIEW",
        "SECURITY_REVIEW",
        "LEXICAL_REVIEW",
        "REVIEW_AGGREGATION",
        "DEVELOPER_FIX",
        "FINAL_MANAGER_REVIEW",
        "READY_FOR_HUMAN_REVIEW",
      ]) {
        expect(i18n.global.te(`workboard.taskTitle.${kind}`, locale)).toBe(
          true,
        );
      }
    },
  );
  it("не придумывает исполнителя процесса и не показывает отсутствие API-поля", async () => {
    const html = await render(
      fixture({
        ref: "run_root",
        rootRunRef: "run_root",
        parentRunRef: undefined,
        title: "SOFTWARE_CHANGE",
        target: {
          type: "WORKFLOW",
          ref: "workflow_own",
          displayName: "Процесс разработки",
          version: 1,
        },
      }),
    );
    expect(html).toContain("Изменение ПО");
    expect(html).not.toContain("Исполнитель");
    expect(html).not.toContain("не передан API");
    expect(html).toContain("Владелец");
    expect(html).toContain("/projects/project_own/runs/run_root");
  });
  it("скрывает строку исполнителя без непустого имени сотрудника", async () => {
    const html = await render(
      fixture({
        target: {
          type: "AGENT",
          ref: "agent_architect",
          displayName: "  ",
          version: 1,
        },
      }),
    );
    expect(html).not.toContain("Исполнитель");
    expect(html).not.toContain("не передан API");
  });
  it.each(["USER_EDITED", "AGENT_PROPOSED"] as const)(
    "сохраняет точное название %s",
    async (titleSource) => {
      expect(await render(fixture({ titleSource }))).toContain(
        "ANALYSIS_AND_ARCHITECTURE",
      );
    },
  );
  it("сохраняет неизвестное серверное название без эвристики", async () => {
    expect(await render(fixture({ title: "UNKNOWN_TASK" }))).toContain(
      "UNKNOWN_TASK",
    );
  });
  it("подписывает источник как Запущено через и показывает его справа от контекста", () => {
    expect(source).toContain('$t("common.source")');
    expect(source).toContain("sourceIcon");
    expect(source).toContain("runs.source.${run.source}");
    expect(source).toContain("run-work-item__aside");
  });
});
