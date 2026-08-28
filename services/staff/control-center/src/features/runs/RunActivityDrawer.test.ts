import { renderToString } from "@vue/server-renderer";
import { createSSRApp, h } from "vue";
import { createI18n } from "vue-i18n";
import { describe, expect, it } from "vitest";

import RunActivityDrawer from "@/features/runs/RunActivityDrawer.vue";
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
  title: "Подготовить отчёт",
  titleSource: "USER_EDITED",
  activitySummary: "Аналитик собирает данные для отчёта",
  inputSummary: "Проверь квартальный отчёт",
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
  createdAt: "2026-08-28T08:00:00Z",
  nextActions: [],
};

const node: RunNode = {
  ref: "nod_agent",
  runRef: run.ref,
  type: "AGENT_EXECUTION",
  state: "RUNNING",
  displayName: "Аналитик продаж",
  attempt: 1,
  artifactRefs: [],
  childRunRefs: [],
  createdAt: "2026-08-28T08:00:01Z",
  nextActions: [],
};
const toolNode: RunNode = {
  ref: "nod_tool",
  runRef: run.ref,
  parentNodeRef: node.ref,
  type: "EXTERNAL_ACTION",
  state: "SUCCEEDED",
  displayName: "Поиск по файлам",
  role: "Файлы Проекта",
  attempt: 1,
  progressSummary: "Найдено 4 фрагмента",
  artifactRefs: [],
  childRunRefs: [],
  createdAt: "2026-08-28T08:00:01Z",
  nextActions: [],
};

const event: PresentedRunEvent = {
  ref: "evt_progress",
  runRef: run.ref,
  sequence: 1,
  type: "TURN_PROGRESS",
  nodeRef: node.ref,
  summary: "Собираю данные",
  displaySummary: "Собираю данные",
  nodeState: "RUNNING",
  occurredAt: "2026-08-28T08:00:02Z",
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

async function render(nodes: RunNode[] = [node]): Promise<string> {
  const app = createSSRApp({
    render: () =>
      h(RunActivityDrawer, {
        open: true,
        run,
        nodes,
        events: [event],
        initiatorSummary: run.inputSummary,
      }),
  });
  app.use(
    createI18n({
      legacy: false,
      locale: "ru",
      messages: {
        ru: {
          common: {
            all: "Все",
            close: "Закрыть",
            details: "Подробнее",
            unavailable: "Функция временно недоступна",
            noData: "Нет данных",
          },
          runs: {
            activity: "Ход работы",
            context: "Контекст узла",
            noNodeActivity: "Сообщений пока нет",
            nodeTypes: { EXTERNAL_ACTION: "Внешнее действие" },
          },
          states: { RUNNING: "Выполняется", SUCCEEDED: "Завершено" },
        },
      },
    }),
  );
  return renderToString(app);
}

describe("RunActivityDrawer", () => {
  it("разделяет сообщения инициатора, агента и unavailable tool-call блок", async () => {
    const html = await render();

    expect(html).toContain("Владелец");
    expect(html).toContain("Проверь квартальный отчёт");
    expect(html).toContain("Аналитик продаж");
    expect(html).toContain("Собираю данные");
    expect(html).toContain("run-tool-call--unavailable");
    expect(html).toContain("Функция временно недоступна");
  });

  it("показывает EXTERNAL_ACTION отдельным tool-call блоком", async () => {
    const html = await render([node, toolNode]);

    expect(html).toContain("Поиск по файлам");
    expect(html).toContain("Найдено 4 фрагмента");
    expect(html).toContain("Завершено");
    expect(html).not.toContain("run-tool-call--unavailable");
  });
});
