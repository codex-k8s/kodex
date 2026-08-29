import { renderToString } from "@vue/server-renderer";
import { createSSRApp, h } from "vue";
import { createI18n } from "vue-i18n";
import { describe, expect, it } from "vitest";

import RunSessionDetailsDialog from "@/features/runs/RunSessionDetailsDialog.vue";
import type { PresentedRunEvent } from "@/features/runs/run-activity";
import type { Run, RunNode } from "@/shared/api/generated/openapi/types.gen";

const run: Run = {
  ref: "run_example",
  version: 1,
  projectRef: "prj_example",
  sessionRef: "ses_example",
  rootRunRef: "run_example",
  target: {
    type: "AGENT",
    ref: "agt_example",
    displayName: "Аналитик",
    version: 1,
  },
  title: "Подготовка отчёта",
  titleSource: "USER_EDITED",
  activitySummary: "Сбор подтверждённых фактов",
  inputSummary: "Проверь отчёт",
  state: "RUNNING",
  source: "CONTROL_CENTER",
  initiator: { ref: "usr_example", displayName: "Владелец" },
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
  inputArtifactRefs: [],
  artifactRefs: [],
  gateRefs: [],
  createdAt: "2026-08-29T08:00:00Z",
  nextActions: [],
};

const node: RunNode = {
  ref: "nod_agent",
  runRef: run.ref,
  type: "AGENT_EXECUTION",
  state: "RUNNING",
  displayName: "Сессия аналитика",
  role: "Аналитик продаж",
  attempt: 1,
  inputSummary: "Проверь квартальный отчёт",
  progressSummary: "Собираю факты",
  integrationNames: ["Файлы Проекта"],
  artifactRefs: [],
  childRunRefs: [],
  createdAt: "2026-08-29T08:00:01Z",
  startedAt: "2026-08-29T08:00:02Z",
  nextActions: [],
};

const event: PresentedRunEvent = {
  ref: "evt_progress",
  runRef: run.ref,
  sequence: 1,
  type: "TURN_PROGRESS",
  nodeRef: node.ref,
  summary: "ignored",
  displaySummary: "Собираю подтверждённые факты",
  actor: { kind: "AGENT", ref: "agt_example", name: "Аналитик продаж" },
  occurredAt: "2026-08-29T08:00:03Z",
  graphRevision: 1,
  run: {
    ref: run.ref,
    version: 1,
    state: "RUNNING",
    graphRevision: 1,
    lastEventSequence: 1,
    usage: run.usage,
    artifactRefs: [],
    gateRefs: [],
    nextActions: [],
  },
};

describe("RunSessionDetailsDialog", () => {
  it("показывает доступные launch данные и честные runtime/prompt states", async () => {
    const app = createSSRApp({
      render: () =>
        h(RunSessionDetailsDialog, {
          run,
          node,
          nodes: [node],
          events: [event],
          artifacts: [],
        }),
    });
    app.use(
      createI18n({
        legacy: false,
        locale: "ru",
        messages: {
          ru: {
            common: {
              close: "Закрыть",
              status: "Состояние",
              source: "Источник",
              input: "Входные данные",
              empty: "Пусто",
              noData: "Нет данных",
              unavailable: "Функция временно недоступна",
            },
            agents: {
              profile: "Профиль сотрудника",
              runtime: "Модель выполнения",
              instructions: "Инструкции",
              integrations: "Разрешённые интеграции",
              role: "Роль",
            },
            runs: {
              launchSummary: "Что будет запущено",
              waitingForActivity: "Ожидает начала работы",
              attempt: "Попытка {attempt}",
              startedAt: "Начало",
              finishedAt: "Завершение",
              nodeConversation: "Работа ИИ-сотрудника",
              noNodeActivity: "Сообщений пока нет",
              toolParameters: "Безопасные параметры",
              toolResult: "Безопасный результат",
              sessionNode: "Сессия",
              controlNode: "Контрольный этап",
              runContext: "Контекст запуска",
              artifacts: "Результаты и файлы",
              renderedPromptUnavailable:
                "Полностью отрендеренные инструкции недоступны текущему API.",
              source: { CONTROL_CENTER: "Control Center" },
              nodeTypes: { AGENT_EXECUTION: "ИИ-сотрудник" },
            },
            states: { RUNNING: "Выполняется" },
          },
        },
      }),
    );

    const html = await renderToString(app);

    expect(html).toContain("Сессия аналитика");
    expect(html).toContain("Проверь квартальный отчёт");
    expect(html).toContain("Control Center");
    expect(html).toContain("Файлы Проекта");
    expect(html).toContain("Модель выполнения");
    expect(html).toContain("Инструкции");
    expect(html).toContain(
      "Полностью отрендеренные инструкции недоступны текущему API.",
    );
    expect(html).toContain("Пусто");
    expect(html).toContain("Собираю подтверждённые факты");
    expect(html).not.toContain("ses_example");
  });
});
