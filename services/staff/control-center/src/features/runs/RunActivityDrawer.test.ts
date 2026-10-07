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
  execution: {
    runRef: run.ref,
    nodeRef: node.ref,
    sessionRef: run.sessionRef,
    turnRef: "trn_example",
    turnNumber: 1,
    attempt: 1,
  },
  message: {
    source: { origin: "ORDINARY" as const },
    ref: "msg_commentary",
    phase: "COMMENTARY",
    revision: 1,
    text: "Собираю данные",
  },
  runRef: run.ref,
  sequence: 1,
  type: "TURN_PROGRESS",
  messageKind: "INTERMEDIATE_MESSAGE",
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

function required<T>(value: T | undefined): T {
  if (value === undefined) throw new Error("Required test fixture is missing");
  return value;
}

async function render(
  nodes: RunNode[] = [node],
  events: PresentedRunEvent[] = [event],
  artifacts: Artifact[] = [],
  initialNodeRef?: string,
  initiatorSummary = run.inputSummary ?? "",
  currentRun: Run = run,
  activityRuns: Run[] = [],
): Promise<string> {
  const app = createSSRApp({
    render: () =>
      h(RunActivityDrawer, {
        open: true,
        run: currentRun,
        activityRuns,
        nodes,
        events,
        artifacts,
        initiatorSummary,
        initialNodeRef,
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
            sessionFilter: "Сессия",
            allSessions: "Все сессии",
            activityItemCount: "Записей: {count}",
            noNodeActivity: "Сообщений пока нет",
            toolParameters: "Безопасные параметры",
            toolResult: "Безопасный результат",
            artifactUnavailable: "Описание файла недоступно",
            toolDuration: "Длительность: {duration} мс",
            expandMessage: "Показать полностью",
            collapseMessage: "Свернуть",
            earlierServiceHistory: "Показать предыдущие служебные записи",
            toolGroup: "Вызовы инструментов: {count}",
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
  const pinnedNode = { ...node, turnRef: "trn_example" };
  const completedTool: PresentedRunEvent = {
    ...event,
    ref: "evt_tool_completed",
    sequence: 2,
    message: undefined,
    type: "TOOL_CALL_RECORDED",
    messageKind: "TOOL_CALL",
    toolCall: {
      ref: "call_completed",
      tool: "files.lookup",
      revision: 2,
      state: "SUCCEEDED",
      safeParameters: {},
      safeResult: "Найдены материалы",
      durationMs: 10,
      auditRef: "audit_completed",
    },
  };

  it("между инструментами показывает один индикатор на ответе, сохраняя завершённый tool", async () => {
    const html = await render([pinnedNode], [event, completedTool]);
    expect(html.match(/class="run-transcript__work"/g)).toHaveLength(1);
    expect(html.indexOf('class="run-transcript__work"')).toBeLessThan(
      html.indexOf("files.lookup"),
    );
    expect(html).toContain("Найдены материалы");
    expect(html).toContain('data-state="SUCCEEDED"');
    const nextTool = {
      ...completedTool,
      ref: "evt_next_tool",
      sequence: 3,
      toolCall: {
        ...required(completedTool.toolCall),
        ref: "call_next",
        state: "RUNNING" as const,
        revision: 1,
      },
    };
    const running = await render(
      [pinnedNode],
      [event, completedTool, nextTool],
    );
    expect(running.match(/class="run-transcript__work"/g)).toHaveLength(1);
    expect(running.indexOf('class="run-transcript__work"')).toBeGreaterThan(
      running.indexOf("Собираю данные"),
    );
  });

  it("показывает только текущий callback attempt узла независимо от retry attempt run", async () => {
    const callbackNode: RunNode = {
      ...pinnedNode,
      ref: "nod_callback",
      turnRef: "trn_callback",
      attempt: 2,
    };
    const previousFinal: PresentedRunEvent = {
      ...event,
      ref: "evt_previous_final",
      sequence: 2,
      nodeState: "SUCCEEDED",
      message: {
        ...required(event.message),
        ref: "msg_previous_final",
        phase: "FINAL",
        text: "Старый ход завершён",
      },
    };
    const callbackEvents: PresentedRunEvent[] = [event, completedTool].map(
      (entry, index) => ({
        ...entry,
        ref: `evt_callback_${String(index)}`,
        sequence: 3 + index,
        nodeRef: callbackNode.ref,
        execution: {
          ...required(entry.execution),
          nodeRef: callbackNode.ref,
          turnRef: required(callbackNode.turnRef),
          turnNumber: 2,
          attempt: callbackNode.attempt,
        },
        message: entry.message
          ? {
              ...entry.message,
              ref: "msg_callback_commentary",
              text: "Продолжаю после callback",
            }
          : undefined,
      }),
    );
    const html = await render(
      [{ ...pinnedNode, state: "SUCCEEDED" }, callbackNode],
      [event, previousFinal, ...callbackEvents],
    );
    expect(run.attempt).toBe(1);
    expect(html.match(/class="run-transcript__work"/g)).toHaveLength(1);
    const indicator = html.indexOf('class="run-transcript__work"');
    expect(indicator).toBeGreaterThan(html.indexOf("Старый ход завершён"));
    expect(indicator).toBeLessThan(html.indexOf("Продолжаю после callback"));
    expect(indicator).toBeLessThan(html.indexOf("files.lookup"));
    expect(html).toContain('data-state="SUCCEEDED"');
  });

  it("между инструментами сохраняет компактный индикатор без commentary", async () => {
    const started: PresentedRunEvent = {
      ...event,
      message: undefined,
      type: "TURN_STARTED",
      messageKind: "STATE",
      summary: "i18n:MODEL_REQUEST_RUNNING",
      displaySummary: "i18n:MODEL_REQUEST_RUNNING",
    };
    const html = await render([pinnedNode], [started, completedTool]);
    expect(html.match(/class="run-transcript__work"/g)).toHaveLength(1);
    expect(html).toContain('data-state="SUCCEEDED"');
  });

  it("сохраняет прежний предикат индикатора системного помощника", async () => {
    const html = await render(
      [pinnedNode],
      [event, completedTool],
      [],
      undefined,
      "",
      { ...run, source: "SYSTEM_ASSISTANT" },
    );
    expect(html).not.toContain('class="run-transcript__work"');
    expect(html).toContain('data-state="SUCCEEDED"');
  });

  it.each([
    "SUCCEEDED",
    "FAILED",
    "CANCELLED",
    "WAITING_HUMAN",
    "CANCELLING",
  ] as const)("убирает индикатор при состоянии run %s", async (state) => {
    const html = await render(
      [pinnedNode],
      [event, completedTool],
      [],
      undefined,
      "",
      { ...run, state },
    );
    expect(html).not.toContain('class="run-transcript__work"');
  });

  it.each([
    { state: "SUCCEEDED" as const },
    { state: "FAILED" as const },
    { state: "CANCELLED" as const },
    { turnRef: "trn_other" },
    { attempt: 2 },
    { runRef: "run_child" },
    { turnRef: undefined },
  ])(
    "не выводит индикатор для terminal/другого выбранного узла %j",
    async (change) => {
      const html = await render(
        [{ ...pinnedNode, ...change }],
        [event, completedTool],
        [],
        pinnedNode.ref,
      );
      expect(html).not.toContain('class="run-transcript__work"');
    },
  );

  it.each([
    { sessionRef: "ses_other" },
    { attempt: 2 },
    { turnRef: "trn_other" },
    { runRef: "run_other" },
  ])("не переносит индикатор из другого execution %j", async (change) => {
    const foreign = [event, completedTool].map((entry) => ({
      ...entry,
      execution: { ...required(entry.execution), ...change },
    }));
    const html = await render([pinnedNode], foreign);
    expect(html).not.toContain('class="run-transcript__work"');
  });

  it("FINAL закрывает индикатор даже до обновления RUNNING snapshot", async () => {
    const final = {
      ...event,
      ref: "evt_final",
      sequence: 3,
      message: {
        ...required(event.message),
        ref: "msg_final",
        phase: "FINAL" as const,
      },
    };
    const html = await render([pinnedNode], [event, completedTool, final]);
    expect(html).not.toContain('class="run-transcript__work"');
  });

  it("использует exact child snapshot и не наследует состояние root", async () => {
    const child: Run = {
      ...run,
      ref: "run_child",
      sessionRef: "ses_child",
      parentRunRef: run.ref,
    };
    const childNode = { ...pinnedNode, runRef: child.ref };
    const childEvents = [event, completedTool].map((entry) => ({
      ...entry,
      execution: {
        ...required(entry.execution),
        runRef: child.ref,
        sessionRef: child.sessionRef,
      },
    }));
    const html = await render(
      [childNode],
      childEvents,
      [],
      childNode.ref,
      "",
      run,
      [child],
    );
    expect(html.match(/class="run-transcript__work"/g)).toHaveLength(1);
    for (const snapshots of [[], [{ ...child, state: "SUCCEEDED" as const }]]) {
      const closed = await render(
        [childNode],
        childEvents,
        [],
        childNode.ref,
        "",
        run,
        snapshots,
      );
      expect(closed).not.toContain('class="run-transcript__work"');
    }
  });

  it("terminal event закрывает fallback, даже если это не компактная служебная запись", async () => {
    const terminal = {
      ...event,
      ref: "evt_terminal",
      sequence: 3,
      message: undefined,
      nodeState: "FAILED" as const,
      type: "TURN_PROGRESS" as const,
      summary: "Сбой",
      displaySummary: "Сбой",
    };
    const html = await render([pinnedNode], [event, completedTool, terminal]);
    expect(html).not.toContain('class="run-transcript__work"');
  });

  it("отображает commentary, вызов с итоговым revision и final в общей хронологии", async () => {
    const started: PresentedRunEvent = {
      ...event,
      ref: "evt_tool_started",
      sequence: 2,
      message: undefined,
      toolCall: {
        ref: "call_progress",
        tool: "files.lookup",
        revision: 1,
        state: "RUNNING",
        safeParameters: {},
        safeResult: "",
        durationMs: 0,
        auditRef: "audit_progress",
      },
    };
    const completed: PresentedRunEvent = {
      ...started,
      ref: "evt_tool_done",
      sequence: 4,
      toolCall: {
        ...required(started.toolCall),
        revision: 2,
        state: "SUCCEEDED",
        safeResult: "Найдены материалы",
      },
    };
    const final: PresentedRunEvent = {
      ...event,
      ref: "evt_final",
      sequence: 5,
      message: {
        source: { origin: "ORDINARY" as const },
        ref: "msg_final",
        phase: "FINAL",
        revision: 1,
        text: "Итоговый полный ответ",
      },
    };
    const html = await render([node], [final, completed, started, event]);
    expect(html.indexOf("Собираю данные")).toBeLessThan(
      html.indexOf("files.lookup"),
    );
    expect(html.indexOf("files.lookup")).toBeLessThan(
      html.indexOf("Итоговый полный ответ"),
    );
    expect(html.match(/files.lookup/g)).toHaveLength(1);
    expect(html).toContain("Найдены материалы");
    expect(html).toContain('data-turn-ref="trn_example"');
  });

  it("выводит только безопасные поля вызова, не raw args/output или hidden reasoning", async () => {
    const toolCall = Object.assign(
      {
        ref: "call_safe",
        tool: "files.read",
        revision: 1,
        state: "SUCCEEDED" as const,
        safeParameters: { file: "report.md" },
        safeResult: "Безопасный результат",
        durationMs: 20,
        auditRef: "audit_safe",
      },
      {
        arguments: "RAW_ARGUMENT_SENTINEL",
        output: "RAW_OUTPUT_SENTINEL",
        reasoning: "HIDDEN_REASONING_SENTINEL",
        headers: "HEADER_SENTINEL",
      },
    );
    const html = await render(
      [node],
      [{ ...event, message: undefined, toolCall }],
    );
    expect(html).toContain("report.md");
    for (const forbidden of [
      "RAW_ARGUMENT_SENTINEL",
      "RAW_OUTPUT_SENTINEL",
      "HIDDEN_REASONING_SENTINEL",
      "HEADER_SENTINEL",
    ])
      expect(html).not.toContain(forbidden);
  });

  it("выделяет старую историю без execution вместо приписывания текущему ходу", async () => {
    const html = await render(
      [node],
      [{ ...event, execution: undefined, message: undefined }],
    );
    expect(html).toContain("Показать предыдущие служебные записи");
    expect(html).not.toContain("Служебная история без точной привязки");
    expect(html).not.toContain('data-turn-ref="trn_example"');
    expect(html).not.toContain('data-phase="COMMENTARY"');
  });

  it("сворачивает длинную серию tools, сохраняя названия и раскрываемые безопасные результаты", async () => {
    const toolEvents: PresentedRunEvent[] = Array.from(
      { length: 5 },
      (_, index) => ({
        ...event,
        ref: `evt_tool_${String(index)}`,
        sequence: index + 2,
        message: undefined,
        toolCall: {
          ref: `call_${String(index)}`,
          tool: `files.operation_${String(index)}`,
          revision: 1,
          state: "SUCCEEDED",
          safeParameters: {},
          safeResult: `Результат ${String(index)}`,
          durationMs: 10,
          auditRef: `audit_${String(index)}`,
        },
      }),
    );
    const html = await render([node], toolEvents, [], undefined, "");
    expect(html).toContain("Вызовы инструментов: 5");
    expect(html).toContain('class="run-transcript__tool-group"');
    expect(html).toContain("files.operation_4");
    expect(html).toContain("Результат 4");
    expect(html).not.toContain("<details open");
  });

  it("разделяет сообщения инициатора и агента без выдуманного tool-call", async () => {
    const html = await render();

    expect(html).toContain("run-activity-drawer__session-filter");
    expect(html).toContain("Все сессии");
    expect(html).toContain("Записей: 2");
    expect(html).toContain("Аналитик продаж · Выполняется");
    expect(html).not.toContain("run-session-strip");
    expect(html).toContain("Аналитик продаж");
    expect(html).toContain("Владелец");
    expect(html).toContain("Проверь квартальный отчёт");
    expect(html).toContain("Аналитик продаж");
    expect(html).toContain("Собираю данные");
    expect(html).not.toContain("run-tool-event");
  });

  it("не выдумывает tool-call блок из одного EXTERNAL_ACTION node", async () => {
    const html = await render([node, toolNode]);

    expect(html).not.toContain("Поиск по файлам");
    expect(html).not.toContain("Найдено 4 фрагмента");
    expect(html).not.toContain("run-tool-event");
  });

  it("показывает параметры и результат записанного tool call", async () => {
    const toolEvent: PresentedRunEvent = {
      ...event,
      ref: "evt_tool",
      type: "TOOL_CALL_RECORDED",
      nodeRef: toolNode.ref,
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
        revision: 1,
        durationMs: 240,
        safeResult: "Найдено 4 фрагмента",
        auditRef: "evt_audit",
      },
    };

    const html = await render([node, toolNode], [toolEvent], [], node.ref);

    expect(html).toContain("run-activity-item--tool");
    expect(html).toContain("project_files.search");
    expect(html).toContain("квартальный отчёт");
    expect(html).toContain("Найдено 4 фрагмента");
    expect(html).toContain("Безопасный результат");
    expect(html).toContain("Длительность: 240 мс");
  });

  it("не выводит пустую длительность неуспешного вызова инструмента", async () => {
    const failedTool: PresentedRunEvent = {
      ...event,
      ref: "evt_failed_tool",
      type: "TOOL_CALL_RECORDED",
      toolCall: {
        ref: "trn_failed_tool",
        tool: "integration.read",
        safeParameters: {},
        state: "FAILED",
        revision: 1,
        safeResult: "",
        auditRef: "evt_failed_audit",
        durationMs: 0,
      },
    };
    if (!failedTool.toolCall) throw new Error("Test tool call is missing");
    Reflect.deleteProperty(failedTool.toolCall, "durationMs");

    const html = await render([node], [failedTool]);

    expect(html).toContain("integration.read");
    expect(html).not.toContain("Длительность:");
  });

  it("сворачивает длинное сообщение инициатора, сохраняя раскрытие", async () => {
    const html = await render();
    expect(html).not.toContain("Показать полностью");
    expect(html).not.toContain("Нет данных");

    const longHtml = await render(
      [node],
      [event],
      [],
      undefined,
      "Проверить отчёт. ".repeat(100),
    );
    expect(longHtml).toContain("run-activity-item__message--collapsed");
    expect(longHtml).toContain("Показать полностью");
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
