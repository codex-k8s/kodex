import { createPinia } from "pinia";
import { readFileSync } from "node:fs";
import { runInNewContext } from "node:vm";
import { transpileModule } from "typescript";
import { createSSRApp } from "vue";
import { createI18n } from "vue-i18n";
import { createMemoryHistory, createRouter } from "vue-router";
import { renderToString } from "@vue/server-renderer";
import { afterAll, beforeAll, describe, expect, it, vi } from "vitest";

import { usePlatformStore } from "@/features/platform/store";
import { useRealtimeStore } from "@/features/realtime/store";
import RunPage from "@/pages/RunPage.vue";
import type { Run } from "@/shared/api/generated/openapi/types.gen";
import { asProblem } from "@/shared/api/problem";
import { serverTokenTranslations } from "@/shared/ui/server-message-catalog";

beforeAll(() => {
  vi.stubGlobal("window", {
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    clearTimeout,
    setTimeout,
  });
});

afterAll(() => vi.unstubAllGlobals());

function messages() {
  return {
    serverMessages: {
      RUN_COORDINATION_ROLE: "Координатор запуска",
      REQUIRED_WORKFLOW_FAILED:
        serverTokenTranslations.REQUIRED_WORKFLOW_FAILED[0],
    },
    common: {
      status: "Состояние",
      result: "Результат",
      error: "Ошибка",
      empty: "Здесь пока ничего нет",
      continue: "Продолжить",
      send: "Отправить",
      retry: "Повторить",
      details: "Подробнее",
      close: "Закрыть",
      unavailable: "Недоступно",
      unknownStatus: "Неизвестное состояние",
      yes: "Да",
      no: "Нет",
      sessionStorageUnavailable:
        "Хранилище этого диалога недоступно. Продолжение сейчас невозможно.",
    },
    runs: {
      title: "Запуски",
      queued: "Задача поставлена в очередь",
      graph: "Граф выполнения",
      graphNodes: "Узлы: {count}",
      graphEdges: "Связи: {count}",
      runtimeProgress: {
        workloadScheduled: "Задание передано исполнителю",
      },
      activity: "Ход работы",
      context: "Контекст узла",
      workspaceTools: "Инструменты запуска",
      connections: "Связи графа",
      artifacts: "Результаты и файлы",
      incidents: "Диагностика",
      noIncidents: "Активных инцидентов нет",
      cancel: "Отменить",
      retry: "Повторить",
      attempt: "Попытка {attempt}",
      previousAttempt: "Предыдущая попытка",
      continueTask: "Дополнительное задание",
      live: "Данные поступают в реальном времени",
      historyComplete: "История запуска завершена",
      streamConnecting: "Подключаем обновления…",
      streamRecovering: "Восстанавливаем обновления…",
      streamOffline: "Обновления недоступны · показаны последние данные",
      noEvents: "Событий пока нет",
      callback: "Ответ дочернего запуска",
      childRuns: "Дочерние запуски",
      openChildRun: "Открыть дочерний запуск",
      startedAt: "Начало",
      finishedAt: "Завершение",
      nodeConversation: "Работа ИИ-сотрудника",
      noNodeActivity: "Сообщений пока нет",
      usage: {
        title: "Использование токенов",
        total: "Всего",
        input: "Вход",
        cached: "Из кэша",
        output: "Выход",
        reasoning: "Рассуждение",
        contextWindow: "Контекст",
      },
      graphControls: "Управление графом",
      zoom: "Масштаб",
      zoomIn: "Увеличить",
      zoomOut: "Уменьшить",
      fitGraph: "Вместить",
      minimap: "Мини-карта графа",
      waitingForActivity: "Ожидает начала работы",
      sessionNode: "Сессия",
      controlNode: "Контрольный этап",
      toolResult: "Безопасный результат",
      artifactUnavailable: "Описание файла недоступно",
      source: {
        CONTROL_CENTER: "Control Center",
        AGENT_DELEGATION: "Делегирование",
      },
      nodeTypes: {
        ROOT_PROCESS: "Основной процесс",
        AGENT_EXECUTION: "ИИ-сотрудник",
        HUMAN_GATE: "Решение человека",
        EXTERNAL_ACTION: "Внешнее действие",
      },
    },
    states: {
      COMPLETED: "Готово",
      RUNNING: "Выполняется",
      WAITING: "Ожидает",
      SUCCEEDED: "Завершён",
      FAILED: "Ошибка",
      NEEDS_ATTENTION: "Требует внимания",
      OUTCOME_NEEDS_ATTENTION: "Требует внимания",
      CLEAN: "Проверен",
    },
  };
}

describe("RunPage runtime presentation", () => {
  function transcriptReader() {
    const source = readFileSync(
      new URL("./RunPage.vue", import.meta.url),
      "utf8",
    );
    const body = source.slice(
      source.indexOf("let transcriptScope:"),
      source.indexOf("const gateDialogOpen"),
    );
    const owner = new AbortController();
    const pending: Array<{
      resolve: () => void;
      signal: AbortSignal;
      ref: string;
    }> = [];
    const history: Record<string, number> = {};
    const run = {
      value: {
        ref: "run_fixture",
        rootRunRef: "run_fixture",
        projectRef: "project_fixture",
      },
    };
    const routeProjectRef = { value: undefined as string | undefined };
    const graph = { value: { runRef: "run_fixture", sequence: 13 } };
    const activityOpen = { value: false };
    const nodeDetailsOpen = { value: false };
    const transcriptReadProblem = { value: undefined as unknown };
    const loadRunTranscript = vi.fn(
      (ref: string, signal: AbortSignal) =>
        new Promise<void>((resolve) => pending.push({ ref, signal, resolve })),
    );
    const platform = {
      bootstrap: { organizationRef: "org_fixture" },
      runTranscriptSequence: (ref: string) => history[ref] ?? 0,
      loadRunTranscript,
    };
    const output = transpileModule(
      body + "\n({refreshTranscriptHistory, closeTranscriptRead});",
      {},
    ).outputText;
    const api = runInNewContext(output, {
      run,
      routeProjectRef,
      graph,
      platform,
      activityOpen,
      nodeDetailsOpen,
      transcriptReadProblem,
      ownerRequestSignal: () => owner.signal,
      AbortController,
      AbortSignal,
      asProblem,
    }) as { refreshTranscriptHistory(): void; closeTranscriptRead(): void };
    return {
      api,
      run,
      routeProjectRef,
      graph,
      platform,
      history,
      activityOpen,
      nodeDetailsOpen,
      owner,
      pending,
      loadRunTranscript,
      transcriptReadProblem,
      source,
    };
  }

  it("открытый modal догружает пропущенную историю после readiness snapshot, без повторного graph read", async () => {
    const state = transcriptReader();
    state.api.refreshTranscriptHistory();
    expect(state.loadRunTranscript).not.toHaveBeenCalled();
    state.history.run_fixture = 2;
    state.nodeDetailsOpen.value = true;
    state.api.refreshTranscriptHistory();
    expect(state.loadRunTranscript).toHaveBeenCalledTimes(1);
    expect(state.pending[0]?.ref).toBe("run_fixture");
    state.history.run_fixture = 13;
    state.pending[0]?.resolve();
    await vi.waitFor(() => state.api.refreshTranscriptHistory());
    expect(state.loadRunTranscript).toHaveBeenCalledTimes(1);
    expect(state.source).toContain(':history-problem="transcriptProblem"');
    expect(state.source).toContain("closeTranscriptRead();");
  });

  it("объединяет graph cursor изменения в один inflight и дочитывает последний cursor", async () => {
    const state = transcriptReader();
    state.activityOpen.value = true;
    state.api.refreshTranscriptHistory();
    state.graph.value.sequence = 14;
    state.api.refreshTranscriptHistory();
    state.graph.value.sequence = 15;
    state.api.refreshTranscriptHistory();
    expect(state.loadRunTranscript).toHaveBeenCalledTimes(1);
    state.history.run_fixture = 13;
    state.pending[0]?.resolve();
    await vi.waitFor(() =>
      expect(state.loadRunTranscript).toHaveBeenCalledTimes(2),
    );
    state.history.run_fixture = 15;
    state.pending[1]?.resolve();
    await vi.waitFor(() => state.api.refreshTranscriptHistory());
    expect(state.loadRunTranscript).toHaveBeenCalledTimes(2);
  });

  it.each(["close", "route", "project", "owner"] as const)(
    "отменяет собственный read при %s и не применяет old completion",
    async (change) => {
      const state = transcriptReader();
      state.activityOpen.value = true;
      state.api.refreshTranscriptHistory();
      if (change === "close") state.activityOpen.value = false;
      else if (change === "route") {
        state.run.value = {
          ref: "run_newfixture",
          rootRunRef: "run_newfixture",
          projectRef: "project_fixture",
        };
        state.graph.value = { runRef: "run_newfixture", sequence: 0 };
      } else if (change === "project")
        state.routeProjectRef.value = "project_foreign";
      else state.owner.abort();
      state.api.refreshTranscriptHistory();
      expect(state.pending[0]?.signal.aborted).toBe(true);
      state.pending[0]?.resolve();
      await Promise.resolve();
      await Promise.resolve();
      state.api.refreshTranscriptHistory();
      expect(state.loadRunTranscript).toHaveBeenCalledTimes(1);
      state.api.closeTranscriptRead();
    },
  );

  it("ошибка history не создаёт repeat loop; covered/foreign/malformed snapshots не читаются", async () => {
    const state = transcriptReader();
    state.activityOpen.value = true;
    state.loadRunTranscript.mockRejectedValueOnce(
      new Error("Synthetic history unavailable"),
    );
    state.api.refreshTranscriptHistory();
    await vi.waitFor(() =>
      expect(state.transcriptReadProblem.value).toBeDefined(),
    );
    for (let i = 0; i < 3; i++) state.api.refreshTranscriptHistory();
    expect(state.loadRunTranscript).toHaveBeenCalledTimes(1);
    state.api.closeTranscriptRead();
    state.history.run_fixture = 13;
    state.api.refreshTranscriptHistory();
    expect(state.loadRunTranscript).toHaveBeenCalledTimes(1);
    state.history.run_fixture = 0;
    state.graph.value.runRef = "run_foreignfixture";
    state.api.refreshTranscriptHistory();
    state.graph.value = { runRef: "run_fixture", sequence: -1 };
    state.api.refreshTranscriptHistory();
    expect(state.loadRunTranscript).toHaveBeenCalledTimes(1);
  });

  it.each(["ERROR", "PURGED", "late ERROR"] as const)(
    "не продолжает сессию при %s и сохраняет ввод после async подготовки",
    async (state) => {
      const source = readFileSync(
        new URL("./RunPage.vue", import.meta.url),
        "utf8",
      );
      const body = source.slice(
        source.indexOf("async function continueRun()"),
        source.indexOf("async function decide("),
      );
      const sessionStorageBlocker = {
        value: state === "late ERROR" ? undefined : state,
      };
      const turn = { value: "Сохранённый ввод" };
      const busy = { value: false };
      const continueSession = vi.fn();
      const finalize = vi.fn(() => {
        sessionStorageBlocker.value = "ERROR";
        return Promise.resolve("attach_fixture");
      });
      const clear = vi.fn();
      const context = {
        run: {
          value: {
            ref: "run_fixture",
            sessionRef: "ses_fixture",
            projectRef: "prj_fixture",
            nextActions: ["ADD_TURN"],
          },
        },
        sessionStorageBlocker,
        runSessionStorageBlocker: () => sessionStorageBlocker.value,
        turn,
        busy,
        turnAttachmentState: { value: { ready: true } },
        mutationGeneration: 1,
        routeProjectRef: { value: "prj_fixture" },
        turnAttachmentComposer: { value: { finalize, clear } },
        selectedNode: { value: undefined },
        problem: { value: undefined },
        mutationCurrent: () => true,
        platform: { continueSession },
        asProblem,
      };
      await runInNewContext(`${body}; continueRun()`, context);
      expect(continueSession).not.toHaveBeenCalled();
      expect(clear).not.toHaveBeenCalled();
      expect(turn.value).toBe("Сохранённый ввод");
      expect(busy.value).toBe(false);
      expect(finalize).toHaveBeenCalledTimes(state === "late ERROR" ? 1 : 0);
      expect(source).toContain(
        "runSessionStorageBlocker(run.value, platform.bootstrap?.organizationRef)",
      );
      expect(source).toContain("Boolean(sessionStorageBlocker)");
    },
  );
  it("разделяет lifecycle и outcome и не показывает сырые runtime данные", async () => {
    const pinia = createPinia();
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: "/runs/:runRef", component: RunPage }],
    });
    await router.push("/runs/run_public_example");
    await router.isReady();

    const platform = usePlatformStore(pinia);
    const runRef = "run_public_example";
    const currentRun: Run = {
      ref: runRef,
      version: 2,
      projectRef: "prj_public_example",
      sessionRef: "ses_public_example",
      rootRunRef: runRef,
      target: {
        type: "AGENT",
        ref: "agt_public_example",
        displayName: "Аналитик",
        version: 1,
      },
      title: "Проверка отчёта",
      titleSource: "USER_EDITED",
      activitySummary: "Отчёт подготовлен",
      state: "SUCCEEDED",
      source: "CONTROL_CENTER",
      initiator: { ref: "usr_public_example", displayName: "Владелец" },
      attempt: 1,
      graphRevision: 2,
      lastEventSequence: 1,
      usage: {
        totalTokens: 1700,
        inputTokens: 1400,
        cachedInputTokens: 900,
        cacheWriteInputTokens: 100,
        outputTokens: 300,
        reasoningOutputTokens: 120,
        modelContextWindow: 200000,
      },
      currentActivity: "MODEL_REQUEST_RUNNING run_secret_header_reference",
      resultSummary: JSON.stringify({
        status: "blocked",
        report: "Доступ к файлу ограничен",
        run_ref: "run_secret_internal_reference",
      }),
      safeErrorCode: "REPORT_BLOCKED",
      artifactRefs: [],
      gateRefs: [],
      createdAt: "2026-08-27T12:00:00Z",
      finishedAt: "2026-08-27T12:01:00Z",
      nextActions: [],
      incidents: [],
    };
    platform.runs[runRef] = currentRun;
    platform.graphs[runRef] = {
      runRef,
      revision: 2,
      sequence: 1,
      nodes: [
        {
          ref: "nod_public_example",
          runRef,
          type: "AGENT_EXECUTION",
          state: "SUCCEEDED",
          displayName: "Аналитик",
          role: "i18n:RUN_COORDINATION_ROLE",
          attempt: 1,
          progressSummary: "WORKLOAD_SCHEDULED",
          artifactRefs: [],
          childRunRefs: [],
          createdAt: "2026-08-27T12:00:00Z",
          finishedAt: "2026-08-27T12:01:00Z",
          nextActions: [],
        },
      ],
      edges: [],
    };
    platform.events[runRef] = {
      1: {
        ref: "evt_public_example",
        runRef,
        sequence: 1,
        type: "TURN_PROGRESS",
        nodeRef: "nod_public_example",
        summary: "MODEL_REQUEST_RUNNING",
        progress: "WORKLOAD_SCHEDULED run_secret_internal_reference",
        runState: "SUCCEEDED",
        nodeState: "SUCCEEDED",
        occurredAt: "2026-08-27T12:01:00Z",
        graphRevision: 2,
        run: {
          ref: runRef,
          version: 2,
          state: "SUCCEEDED",
          graphRevision: 2,
          lastEventSequence: 1,
          usage: currentRun.usage,
          resultSummary: currentRun.resultSummary,
          artifactRefs: [],
          gateRefs: [],
          finishedAt: "2026-08-27T12:01:00Z",
          nextActions: [],
        },
      },
    };
    platform.runProblems[currentRun.ref] = asProblem({
      status: 503,
      code: "RUN_EVENTS_UNAVAILABLE",
      title: "История событий временно недоступна",
      retryable: true,
    });

    const app = createSSRApp(RunPage);
    app.use(pinia);
    app.use(router);
    app.use(
      createI18n({
        legacy: false,
        locale: "ru",
        messages: { ru: messages() },
      }),
    );
    const html = await renderToString(app);

    expect(html).toContain("Завершён");
    expect(html).toContain("status-badge--success");
    expect(html).toContain("Требует внимания");
    expect(html).toContain("Координатор запуска");
    expect(html).toContain("История событий временно недоступна");
    expect(html).toContain("Граф выполнения");
    expect(html).toContain("run-page-body");
    expect(html).toContain("run-workspace");
    expect(html).toContain("run-canvas-summary");
    expect(html).toContain("run-canvas-summary__heading");
    expect(html).toMatch(
      /class="run-canvas-summary__toggle[^"]*"[^>]*aria-expanded="false"[^>]*aria-controls="run-canvas-summary-details"/,
    );
    expect(html).toContain('id="run-canvas-summary-details"');
    expect(html).not.toContain("run-canvas-summary__details--expanded");
    expect(html).toContain("История запуска завершена");
    expect(html).toContain("· #1");
    expect(html).not.toContain("Данные поступают в реальном времени");
    expect(html).toContain("token-usage");
    expect(html).toContain(new Intl.NumberFormat("ru").format(1700));
    expect(html).toContain("graph-legend");
    expect(html).not.toContain("run-bottom");
    expect(html).not.toContain("MODEL_REQUEST_RUNNING");
    expect(html).not.toContain("WORKLOAD_SCHEDULED");
    expect(html).not.toContain("run_secret_internal_reference");
    expect(html).not.toContain("run_secret_header_reference");
    expect(html).not.toContain("i18n:RUN_COORDINATION_ROLE");
    expect(html).not.toContain("{&quot;status&quot;");

    const graphWithoutEvents = platform.graphs[runRef];
    graphWithoutEvents.sequence = 0;
    const noEventsApp = createSSRApp(RunPage);
    noEventsApp.use(pinia);
    noEventsApp.use(router);
    noEventsApp.use(
      createI18n({
        legacy: false,
        locale: "ru",
        messages: { ru: messages() },
      }),
    );
    const noEventsHtml = await renderToString(noEventsApp);
    expect(noEventsHtml).toContain("История запуска завершена");
    expect(noEventsHtml).not.toContain("· #");

    currentRun.state = "RUNNING";
    const realtime = useRealtimeStore(pinia);
    for (const [state, label] of [
      ["connecting", "Подключаем обновления…"],
      ["recovering", "Восстанавливаем обновления…"],
      ["offline", "Обновления недоступны · показаны последние данные"],
      ["live", "Данные поступают в реальном времени"],
    ] as const) {
      realtime.state[runRef] = { state, attempt: 0 };
      const streamApp = createSSRApp(RunPage);
      streamApp
        .use(pinia)
        .use(router)
        .use(
          createI18n({
            legacy: false,
            locale: "ru",
            messages: { ru: messages() },
          }),
        );
      const streamHtml = await renderToString(streamApp);
      expect(streamHtml).toContain(label);
      expect(streamHtml).not.toContain("История запуска завершена");
      if (state !== "live")
        expect(streamHtml).not.toContain("Данные поступают в реальном времени");
    }

    currentRun.state = "FAILED";
    currentRun.safeErrorCode = "REQUIRED_WORKFLOW_FAILED";
    currentRun.safeErrorMessage = "REQUIRED_WORKFLOW_FAILED";
    const failedApp = createSSRApp(RunPage);
    failedApp.use(pinia);
    failedApp.use(router);
    failedApp.use(
      createI18n({ legacy: false, locale: "ru", messages: { ru: messages() } }),
    );
    const failedHtml = await renderToString(failedApp);
    expect(failedHtml).toMatch(
      /Запуск завершён с ошибкой: обязательный дочерний процесс не выполнен\. <code[^>]*>REQUIRED_WORKFLOW_FAILED<\/code>/,
    );
    expect(failedHtml).not.toContain("REQUIRED_WORKFLOW_FAILED <code>");
    expect(failedHtml).not.toContain("i18n:REQUIRED_WORKFLOW_FAILED");
    currentRun.sessionReadiness = {
      sessionRef: currentRun.sessionRef,
      storageState: "ERROR",
      reason: "STORAGE_NOT_LIVE",
    };
    const storageApp = createSSRApp(RunPage);
    storageApp
      .use(pinia)
      .use(router)
      .use(
        createI18n({
          legacy: false,
          locale: "ru",
          messages: { ru: messages() },
        }),
      );
    const storageHtml = await renderToString(storageApp);
    expect(storageHtml).toContain(
      "Хранилище этого диалога недоступно. Продолжение сейчас невозможно.",
    );
    expect(storageHtml).not.toContain("Восстановить хранилище");
  });
});
