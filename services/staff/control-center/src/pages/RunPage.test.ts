import { createPinia } from "pinia";
import { createSSRApp } from "vue";
import { createI18n } from "vue-i18n";
import { createMemoryHistory, createRouter } from "vue-router";
import { renderToString } from "@vue/server-renderer";
import { afterAll, beforeAll, describe, expect, it, vi } from "vitest";

import { usePlatformStore } from "@/features/platform/store";
import RunPage from "@/pages/RunPage.vue";
import type { Run } from "@/shared/api/generated/openapi/types.gen";
import { asProblem } from "@/shared/api/problem";

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
    },
    runs: {
      title: "Запуски",
      queued: "Задача поставлена в очередь",
      graph: "Граф выполнения",
      activity: "Ход работы",
      context: "Контекст узла",
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
    platform.problems.run = asProblem({
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
    expect(html).toContain("ИИ-сотрудник");
    expect(html).toContain("История событий временно недоступна");
    expect(html).toContain("Граф выполнения");
    expect(html).toContain("run-page-body");
    expect(html).toContain("run-workspace");
    expect(html).toContain("run-canvas-summary");
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
  });
});
