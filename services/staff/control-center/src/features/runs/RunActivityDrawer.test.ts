import { renderToString } from "@vue/server-renderer";
import { createSSRApp, h } from "vue";
import { createI18n } from "vue-i18n";
import { describe, expect, it } from "vitest";

import RunActivityDrawer from "@/features/runs/RunActivityDrawer.vue";
import type { PresentedRunEvent } from "@/features/runs/run-activity";
import type {
  Artifact,
  Run,
  RunNode,
} from "@/shared/api/generated/openapi/types.gen";

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

async function render(
  nodes: RunNode[] = [node],
  events: PresentedRunEvent[] = [event],
  artifacts: Artifact[] = [],
): Promise<string> {
  const app = createSSRApp({
    render: () =>
      h(RunActivityDrawer, {
        open: true,
        run,
        nodes,
        events,
        artifacts,
        initiatorSummary: run.inputSummary ?? "",
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
            download: "Скачать",
            details: "Подробнее",
            unavailable: "Функция временно недоступна",
            noData: "Нет данных",
          },
          runs: {
            activity: "Ход работы",
            context: "Контекст узла",
            noNodeActivity: "Сообщений пока нет",
            toolParameters: "Безопасные параметры",
            toolResult: "Безопасный результат",
            artifactUnavailable: "Описание файла недоступно",
            toolDuration: "Длительность: {duration} мс",
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
  it("разделяет сообщения инициатора и агента без выдуманного tool-call", async () => {
    const html = await render();

    expect(html).toContain("Владелец");
    expect(html).toContain("Проверь квартальный отчёт");
    expect(html).toContain("Аналитик продаж");
    expect(html).toContain("Собираю данные");
    expect(html).not.toContain("run-tool-event");
  });

  it("показывает EXTERNAL_ACTION отдельным tool-call блоком", async () => {
    const html = await render([node, toolNode]);

    expect(html).toContain("Поиск по файлам");
    expect(html).toContain("Найдено 4 фрагмента");
    expect(html).toContain("Завершено");
    expect(html).toContain("Функция временно недоступна");
  });

  it("показывает параметры и результат записанного tool call", async () => {
    const toolEvent: PresentedRunEvent = {
      ...event,
      ref: "evt_tool",
      type: "TOOL_CALL_RECORDED",
      messageKind: "TOOL_CALL",
      actor: {
        kind: "AGENT",
        ref: "agt_example",
        name: "Аналитик продаж",
      },
      toolCall: {
        ref: "trn_tool",
        tool: "project_files.search",
        safeParameters: { query: "квартальный отчёт" },
        state: "SUCCEEDED",
        durationMs: 240,
        safeResult: "Найдено 4 фрагмента",
        auditRef: "evt_audit",
      },
    };

    const html = await render([node], [toolEvent]);

    expect(html).toContain("run-activity-item--tool");
    expect(html).toContain("project_files.search");
    expect(html).toContain("квартальный отчёт");
    expect(html).toContain("Найдено 4 фрагмента");
    expect(html).toContain("Безопасный результат");
  });

  it("показывает безопасное описание файла из события", async () => {
    const artifact: Artifact = {
      ref: "art_report",
      version: 2,
      projectRef: run.projectRef,
      runRef: run.ref,
      fileName: "report.md",
      mediaType: "text/markdown",
      sizeBytes: 2048,
      digest: "sha256:example",
      scanState: "CLEAN",
      source: "AGENT_RESULT",
      revision: 2,
      lifecycleState: "ACTIVE",
      agentBindings: [],
      previewAvailable: true,
      createdAt: "2026-08-28T08:00:03Z",
      nextActions: ["DOWNLOAD"],
    };
    const artifactEvent: PresentedRunEvent = {
      ...event,
      ref: "evt_artifact",
      type: "ARTIFACT_AVAILABLE",
      messageKind: "ARTIFACT",
      artifactRef: artifact.ref,
      displaySummary: "Агент подготовил файл",
    };

    const html = await render([node], [artifactEvent], [artifact]);

    expect(html).toContain("report.md");
    expect(html).toContain("text/markdown");
    expect(html).toContain("2 кБ");
    expect(html).toContain("Скачать");
  });
});
