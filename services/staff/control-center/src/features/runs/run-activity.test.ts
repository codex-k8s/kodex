import { describe, expect, it } from "vitest";

import {
  buildRunActivityItems,
  executionKey,
  buildRunTranscriptItems,
  isTranscriptNearBottom,
  publishedRunMessage,
  assistantTurnHasAuthoritativeActivity,
  assistantTurnIsEmptyTerminalReceipt,
  assistantTurnIsDuplicateFailureReceipt,
  isAssistantPlanToolReceipt,
  isSuccessfulIntegrationToolReceipt,
  activeTranscriptItemId,
  assistantTerminalTranscriptScopes,
  assistantTranscriptReplacesWorkingFallback,
  assistantFailureMessageKey,
  presentRunTranscriptItems,
  type RunActivityItem,
  type PresentedRunEvent,
} from "@/features/runs/run-activity";
import type {
  AssistantTurn,
  AssistantConversation,
  Run,
  RunEvent,
  RunNode,
} from "@/shared/api/generated/openapi/types.gen";

describe("закрытая локализация failed результата", () => {
  it.each([
    "PROVIDER_RESULT_UNVERIFIABLE",
    "PROVIDER_RESULT_UNKNOWN",
    "PROVIDER_AUTHENTICATION_REQUIRED",
    "PROVIDER_USAGE_LIMIT_EXCEEDED",
    "PROVIDER_OVERLOADED",
    "PROVIDER_POLICY_DENIED",
    "RUNTIME_CONFIGURATION_STALE",
    "RUNTIME_PROVIDER_UNAVAILABLE",
  ])("локализует только точный FAILED %s", (code) => {
    expect(assistantFailureMessageKey(code, "FAILED")).toBe(
      `serverMessages.${code}`,
    );
    expect(assistantFailureMessageKey(`i18n:${code}`, "FAILED")).toBe(
      `serverMessages.${code}`,
    );
    expect(assistantFailureMessageKey(code, "SUCCEEDED")).toBeUndefined();
    expect(
      assistantFailureMessageKey(`Ошибка ${code}`, "FAILED"),
    ).toBeUndefined();
  });
  it.each(["RUNTIME_WORKLOAD_EXITED", "FUTURE_RUNTIME_ERROR"])(
    "не выводит сырой terminal code %s в сообщении",
    (code) => {
      expect(assistantFailureMessageKey(code, "FAILED")).toBe(
        "workboard.runFailedSummary",
      );
      expect(assistantFailureMessageKey(`i18n:${code}`, "FAILED")).toBe(
        "workboard.runFailedSummary",
      );
      expect(assistantFailureMessageKey(code, "SUCCEEDED")).toBeUndefined();
    },
  );
  it("использует существующий каталог, а не дополнительный список кодов", () => {
    expect(
      assistantFailureMessageKey("INTERACTION_AUTHORITY_CHANGED", "FAILED"),
    ).toBe("serverMessages.INTERACTION_AUTHORITY_CHANGED");
  });
  it("не интерпретирует error payload или обычный текст как код ошибки", () => {
    expect(
      assistantFailureMessageKey("i18n:PROVIDER_FUTURE_ERROR", "FAILED"),
    ).toBe("workboard.runFailedSummary");
    expect(
      assistantFailureMessageKey("Работа завершилась с ошибкой", "FAILED"),
    ).toBeUndefined();
    expect(
      assistantFailureMessageKey(
        '{"error":"PROVIDER_RESULT_UNVERIFIABLE"}',
        "FAILED",
      ),
    ).toBeUndefined();
  });
});

describe("закрытая машинная квитанция инструмента подготовки плана", () => {
  const tool: NonNullable<RunActivityItem["toolCall"]> = {
    ref: "tcl_example",
    tool: "propose_configuration_plan",
    state: "SUCCEEDED",
    revision: 2,
    durationMs: 10,
    safeParameters: {},
    safeResult: "propose_configuration_plan:pln_fixtureplan123",
    auditRef: "aud_example",
  };
  it("опознаёт только exact SUCCEEDED plan receipt, не текст ответа или ошибку", () => {
    expect(isAssistantPlanToolReceipt(tool)).toBe(true);
    for (const state of ["RUNNING", "FAILED", "CANCELLED"] as const)
      expect(isAssistantPlanToolReceipt({ ...tool, state })).toBe(false);
    expect(isAssistantPlanToolReceipt({ ...tool, tool: "unknown_tool" })).toBe(
      false,
    );
    for (const safeResult of [
      "propose_configuration_plan:pln_short",
      "propose_configuration_plan:run_fixtureplan123",
      "propose_configuration_plan:pln_fixtureplan123\nНужно подтверждение",
      `propose_configuration_plan:pln_${"a".repeat(125)}`,
      "TOOL_UNAVAILABLE",
    ])
      expect(isAssistantPlanToolReceipt({ ...tool, safeResult })).toBe(false);
  });
});

describe("закрытая успешная квитанция интеграции", () => {
  const receipt = {
    version: 1,
    invocationRef: "inv_fixture123",
    state: "SUCCEEDED",
    inputSHA256: "a".repeat(64),
  };
  const tool: NonNullable<RunActivityItem["toolCall"]> = {
    ref: "tcl_example",
    tool: "context7_resolve_library_id",
    state: "SUCCEEDED",
    revision: 2,
    durationMs: 10,
    safeParameters: {},
    safeResult: JSON.stringify(receipt),
    auditRef: "aud_example",
  };
  it.each([
    "invoke_integration",
    "context7_resolve_library_id",
    "context7_query_docs",
  ])("отличает техническую квитанцию %s от текста ответа", (name) =>
    expect(isSuccessfulIntegrationToolReceipt({ ...tool, tool: name })).toBe(
      true,
    ),
  );
  it("не скрывает ошибку, unknown tool, произвольный результат или некорректные поля", () => {
    expect(
      isSuccessfulIntegrationToolReceipt({ ...tool, tool: "unknown_tool" }),
    ).toBe(false);
    for (const state of ["RUNNING", "FAILED", "CANCELLED"] as const)
      expect(isSuccessfulIntegrationToolReceipt({ ...tool, state })).toBe(
        false,
      );
    for (const safeResult of [
      "Результат найден",
      "TOOL_UNAVAILABLE",
      "{invalid}",
      "null",
      "[]",
      JSON.stringify({ ...receipt, version: 2 }),
      JSON.stringify({ ...receipt, state: "FAILED" }),
      JSON.stringify({ ...receipt, invocationRef: "run_fixture123" }),
      JSON.stringify({ ...receipt, invocationRef: "inv_short" }),
      JSON.stringify({ ...receipt, inputSHA256: "a".repeat(63) }),
      JSON.stringify({ ...receipt, inputSHA256: "A".repeat(64) }),
      JSON.stringify({ ...receipt, message: "Важный результат" }),
      JSON.stringify({ ...receipt, inputSHA256: "a".repeat(64) }) +
        " Дополнительный текст",
      `{"version":1,"version":1,"invocationRef":"inv_fixture123","state":"SUCCEEDED","inputSHA256":"${"a".repeat(64)}"}`,
    ])
      expect(isSuccessfulIntegrationToolReceipt({ ...tool, safeResult })).toBe(
        false,
      );
  });
});

describe("компактное представление exact хода", () => {
  const execution = {
    runRef: "run_exact",
    nodeRef: "nod_exact",
    sessionRef: "ses_exact",
    turnRef: "trn_exact",
    turnNumber: 1,
    attempt: 1,
  };
  const item = (
    id: string,
    changes: Partial<RunActivityItem> = {},
  ): RunActivityItem => ({
    id,
    kind: "system",
    actor: "Kodex",
    occurredAt: "2026-10-04T10:00:00Z",
    historical: false,
    execution: { ...execution },
    eventType: "TURN_PROGRESS",
    summary: "MODEL_REQUEST_RUNNING",
    state: "RUNNING",
    ...changes,
  });

  it("не показывает пустую завершённую service карточку до догрузки FINAL только для exact закрытого хода", () => {
    const completed = item("completed-empty", {
      summary: "",
      state: "SUCCEEDED",
      messageKind: "FINAL_MESSAGE",
      eventType: "TURN_COMPLETED",
    });
    const scope = executionKey(execution);
    if (!scope) throw new Error("Synthetic execution scope is missing");
    expect(presentRunTranscriptItems([completed], null, [scope])).toEqual([]);
    expect(presentRunTranscriptItems([completed], null)).toHaveLength(1);
    expect(
      presentRunTranscriptItems([completed], null, ["foreign_scope"]),
    ).toHaveLength(1);
    for (const changed of [
      { summary: "Проверено 5 параметров" },
      { state: "FAILED" as const, summary: "" },
      { historical: true },
      { phase: "FINAL" as const, kind: "agent" as const },
    ]) {
      expect(
        presentRunTranscriptItems([{ ...completed, ...changed }], null, [
          scope,
        ]),
      ).toHaveLength(1);
    }
  });

  it("возвращает служебные этапы в details после FINAL, не теряя meaningful старую историю", () => {
    const scope = executionKey(execution);
    if (!scope) throw new Error("Synthetic execution scope is missing");
    const stages = [
      item("start", { summary: "RUN_STARTED", eventType: "TURN_STARTED" }),
      item("progress"),
      item("completed", {
        summary: "",
        state: "SUCCEEDED",
        messageKind: "FINAL_MESSAGE",
        eventType: "TURN_COMPLETED",
      }),
    ];
    const unchanged = JSON.stringify(stages);
    expect(presentRunTranscriptItems(stages, null, [scope])).toEqual([]);
    const final = item("final", {
      kind: "agent",
      phase: "FINAL",
      summary: "Настройки подготовлены",
      state: "SUCCEEDED",
    });
    const result = presentRunTranscriptItems([...stages, final], null, [scope]);
    expect(result).toHaveLength(1);
    expect(result[0]).toMatchObject({
      id: "final",
      summary: final.summary,
      completedServiceHistory: stages,
    });
    expect(JSON.stringify(stages)).toBe(unchanged);
    const meaningful = stages.map((step, index) =>
      index === 1 ? { ...step, summary: "Проверена настройка" } : step,
    );
    expect(presentRunTranscriptItems(meaningful, null, [scope])).toHaveLength(
      1,
    );
  });

  it("сводит запуск и прогресс в одну запись, не меняя исходную историю", () => {
    const items = [
      item("start", { eventType: "TURN_STARTED" }),
      item("schedule", { summary: "WORKLOAD_SCHEDULED" }),
      item("model"),
    ];
    const original = JSON.stringify(items);
    const presented = presentRunTranscriptItems(items);
    expect(presented).toHaveLength(1);
    expect(presented[0]).toMatchObject({
      id: "start",
      working: true,
      serviceHistory: items,
    });
    expect(JSON.stringify(items)).toBe(original);
  });

  it("оставляет USER/COMMENTARY/FINAL/tool на своих местах и активирует только последнее сообщение", () => {
    const items = [
      item("user", { kind: "initiator", phase: "USER" }),
      item("start"),
      item("comment", {
        kind: "agent",
        phase: "COMMENTARY",
        summary: "Проверяю",
      }),
      item("tool", {
        kind: "tool",
        toolCall: {
          ref: "call_exact",
          tool: "CODEX_SHELL",
          state: "RUNNING",
          durationMs: 0,
          revision: 1,
          safeParameters: {},
          safeResult: "",
          auditRef: "audit_exact",
        },
      }),
    ];
    const presented = presentRunTranscriptItems(items);
    expect(presented.map((entry) => entry.id)).toEqual([
      "user",
      "start",
      "comment",
      "tool",
    ]);
    expect(
      presented.filter((entry) => entry.working).map((entry) => entry.id),
    ).toEqual(["tool"]);
    expect(
      presentRunTranscriptItems([
        ...items,
        item("final", {
          kind: "agent",
          phase: "FINAL",
          state: "SUCCEEDED",
          summary: "Готово",
        }),
      ]).some((entry) => entry.working),
    ).toBe(false);
  });

  it("оставляет одну terminal ошибку, предпочитая информативное итоговое событие", () => {
    const items = [
      item("start"),
      item("error", {
        eventType: "TURN_COMPLETED",
        messageKind: "FINAL_MESSAGE",
        state: "FAILED",
        summary: "Провайдер недоступен",
      }),
      item("node", {
        eventType: "NODE_STATE_CHANGED",
        state: "FAILED",
        summary: "Ошибка",
      }),
      item("run", {
        eventType: "RUN_STATE_CHANGED",
        state: "FAILED",
        summary: "Ошибка",
      }),
    ];
    const presented = presentRunTranscriptItems(items);
    expect(presented).toHaveLength(1);
    expect(presented[0]).toMatchObject({
      summary: "Провайдер недоступен",
      state: "FAILED",
      working: false,
      serviceHistory: items,
    });
  });

  it("сводит локализованные HTTP/WS события exact отмены по typed serviceCode", () => {
    const base = required(events[0]);
    const entries = buildRunTranscriptItems([
      {
        ...base,
        message: undefined,
        type: "NODE_STATE_CHANGED",
        messageKind: "STATE",
        nodeState: "CANCELLED",
        summary: "Шаг выполнения отменён",
        serviceCode: "RUN_NODE_CANCELLED",
      },
      {
        ...base,
        ref: "evt_cancel_progress",
        sequence: 3,
        message: undefined,
        type: "TURN_PROGRESS",
        messageKind: "INTERMEDIATE_MESSAGE",
        nodeState: "CANCELLED",
        summary: "Запуск отменён",
        serviceCode: "RUN_CANCELLED",
      },
    ]);
    expect(presentRunTranscriptItems(entries)).toHaveLength(1);
    expect(presentRunTranscriptItems(entries)[0]?.serviceHistory).toEqual(
      entries,
    );
  });

  it("сводит exact отмену узла и машинный intermediate результат в одну запись", () => {
    const cancel = item("node-cancel", {
      eventType: "NODE_STATE_CHANGED",
      messageKind: "STATE",
      state: "CANCELLED",
      summary: "i18n:RUN_NODE_CANCELLED",
      serviceCancellationCode: "RUN_NODE_CANCELLED",
    });
    const progress = item("cancel-progress", {
      eventType: "TURN_PROGRESS",
      messageKind: "INTERMEDIATE_MESSAGE",
      state: "CANCELLED",
      summary: "i18n:RUN_CANCELLED",
      serviceCancellationCode: "RUN_CANCELLED",
    });
    const entries = [item("start"), cancel, progress];
    const before = structuredClone(entries);
    const result = presentRunTranscriptItems(entries);
    expect(result).toHaveLength(1);
    expect(result[0]).toMatchObject({
      state: "CANCELLED",
      summary: progress.summary,
      working: false,
      serviceHistory: entries,
    });
    expect(entries).toEqual(before);
    for (const [key, value] of [
      ["runRef", "run_foreign"],
      ["nodeRef", "nod_foreign"],
      ["sessionRef", "ses_foreign"],
      ["turnRef", "trn_foreign"],
      ["turnNumber", 2],
      ["attempt", 2],
    ] as const)
      expect(
        presentRunTranscriptItems([
          cancel,
          { ...progress, execution: { ...execution, [key]: value } },
        ]),
      ).toHaveLength(2);
    for (const changed of [
      {
        summary: "Доставка отменена, внешний результат пока неизвестен",
        serviceCancellationCode: undefined,
      },
      {
        summary: "i18n:FUTURE_CANCELLATION",
        serviceCancellationCode: undefined,
      },
      { summary: "i18n:RUN_CANCELLED", serviceCancellationCode: undefined },
      { progress: "Есть дополнительный результат" },
      { integrationInvocationRef: "inv_exact" },
      { phase: "COMMENTARY" as const, kind: "agent" as const },
      { execution: { ...execution, attempt: 2 } },
      { historical: true, execution: undefined },
    ])
      expect(
        presentRunTranscriptItems([cancel, { ...progress, ...changed }]),
      ).toHaveLength(2);
  });

  it("сохраняет typed классификацию exact отмены при пользовательском displaySummary", () => {
    const base = required(events[0]);
    const cancelledEvents: PresentedRunEvent[] = [
      {
        ...base,
        message: undefined,
        type: "NODE_STATE_CHANGED",
        messageKind: "STATE",
        nodeState: "CANCELLED",
        summary: "i18n:RUN_NODE_CANCELLED",
        serviceCode: "RUN_NODE_CANCELLED",
        displaySummary: "Этап запуска отменён",
      },
      {
        ...base,
        ref: "evt_cancel_progress",
        sequence: 3,
        message: undefined,
        type: "TURN_PROGRESS",
        messageKind: "INTERMEDIATE_MESSAGE",
        nodeState: "CANCELLED",
        summary: "i18n:RUN_CANCELLED",
        serviceCode: "RUN_CANCELLED",
        displaySummary: "Запуск отменён",
      },
    ];
    const entries = buildRunTranscriptItems(cancelledEvents);
    expect(presentRunTranscriptItems(entries)).toHaveLength(1);
    expect(presentRunTranscriptItems(entries)[0]?.serviceHistory).toEqual(
      entries,
    );
  });

  it("unbound отмена остаётся отдельной readonly историей без догадки о turn/attempt", () => {
    const entry = item("unbound-cancel", {
      historical: true,
      execution: undefined,
      eventType: "RUN_STATE_CHANGED",
      messageKind: "STATE",
      state: "CANCELLED",
      summary: "i18n:RUN_CANCELLED",
      serviceCancellationCode: "RUN_CANCELLED",
    });
    expect(presentRunTranscriptItems([entry])[0]).toMatchObject({
      historical: true,
      execution: undefined,
      serviceHistory: [entry],
    });
    for (const changed of [
      {
        summary: "Отменён запуск, внешняя доставка пока неизвестна",
        serviceCancellationCode: undefined,
      },
      { summary: "i18n:RUN_CANCELLED", serviceCancellationCode: undefined },
      { progress: "Есть результат" },
      { state: "FAILED" as const },
      { eventType: "NODE_STATE_CHANGED" as const },
    ])
      expect(
        presentRunTranscriptItems([{ ...entry, ...changed }])[0]
          ?.serviceHistory,
      ).toBeUndefined();
  });

  it.each([
    ["runRef", "run_foreign"],
    ["nodeRef", "nod_foreign"],
    ["sessionRef", "ses_foreign"],
    ["turnRef", "trn_foreign"],
    ["turnNumber", 2],
    ["attempt", 2],
  ] as const)("не объединяет чужой %s", (key, value) => {
    const entries = presentRunTranscriptItems([
      item("own"),
      item("other", { execution: { ...execution, [key]: value } }),
    ]);
    expect(entries).toHaveLength(2);
    expect(entries.map((entry) => entry.serviceHistory?.length)).toEqual([
      1, 1,
    ]);
    expect(entries.filter((entry) => entry.working)).toHaveLength(1);
  });

  it("не приписывает UNSCOPED историю и не объединяет owner gate или артефакт", () => {
    const entries = [
      item("old-one", { historical: true, execution: undefined }),
      item("old-two", { historical: true, execution: undefined }),
      item("gate", { messageKind: "OWNER_GATE" }),
      item("artifact", { messageKind: "ARTIFACT", artifactRef: "art_exact" }),
    ];
    expect(presentRunTranscriptItems(entries).map((entry) => entry.id)).toEqual(
      entries.map((entry) => entry.id),
    );
    expect(
      presentRunTranscriptItems(entries.slice(0, 2)).some(
        (entry) => entry.working,
      ),
    ).toBe(false);
    expect(
      presentRunTranscriptItems(entries).some((entry) => entry.serviceHistory),
    ).toBe(false);
  });

  it("старые running этапы не получают активный статус после нового turn/attempt", () => {
    const items = [
      item("old"),
      item("current", {
        execution: {
          ...execution,
          turnRef: "trn_next",
          turnNumber: 2,
          attempt: 2,
        },
      }),
    ];
    expect(activeTranscriptItemId(items)).toBe("current");
    expect(
      presentRunTranscriptItems(items)
        .filter((entry) => entry.working)
        .map((entry) => entry.id),
    ).toEqual(["current"]);
  });

  it.each(["SUCCEEDED", "FAILED", "CANCELLED"] as const)(
    "terminal tool %s не получает Working и не возвращает его старому прогрессу",
    (state) => {
      const items = [
        item("service"),
        item("completed", {
          kind: "tool",
          toolCall: {
            ref: "call_completed",
            tool: "get_configuration_catalog",
            state,
            revision: 2,
            durationMs: 10,
            safeParameters: {},
            safeResult: "get_configuration_catalog:completed",
            auditRef: "audit_exact",
          },
        }),
      ];
      expect(activeTranscriptItemId(items)).toBeNull();
      expect(
        presentRunTranscriptItems(items).some((entry) => entry.working),
      ).toBe(false);
    },
  );

  it("переносит только пустые этапы в exact FINAL, сохраняя опубликованный ответ и порядок остальных записей", () => {
    const items = [
      item("user", { kind: "initiator", phase: "USER", summary: "Запрос" }),
      item("start"),
      item("comment", {
        kind: "agent",
        phase: "COMMENTARY",
        summary: "Проверяю",
      }),
      item("progress"),
      item("final", {
        kind: "agent",
        phase: "FINAL",
        state: "SUCCEEDED",
        summary: "Готово\n\nПолный ответ",
      }),
      item("terminal", { eventType: "RUN_STATE_CHANGED", state: "SUCCEEDED" }),
    ];
    const original = JSON.stringify(items);
    const presented = presentRunTranscriptItems(items);
    expect(presented.map((entry) => entry.id)).toEqual([
      "user",
      "comment",
      "final",
    ]);
    expect(presented[2]).toMatchObject({
      kind: "agent",
      phase: "FINAL",
      summary: "Готово\n\nПолный ответ",
      completedServiceHistory: [items[1], items[3], items[5]],
    });
    expect(presented[2]?.serviceHistory).toBeUndefined();
    expect(JSON.stringify(items)).toBe(original);
    expect(presentRunTranscriptItems(items)).toEqual(presented);
  });
  it.each([
    "runRef",
    "nodeRef",
    "sessionRef",
    "turnRef",
    "turnNumber",
    "attempt",
  ] as const)("не переносит этапы в FINAL с чужим %s", (key) => {
    const value = typeof execution[key] === "number" ? 2 : "foreign_ref";
    const presented = presentRunTranscriptItems([
      item("start", { eventType: "TURN_COMPLETED", state: "SUCCEEDED" }),
      item("final", {
        kind: "agent",
        phase: "FINAL",
        state: "SUCCEEDED",
        execution: { ...execution, [key]: value },
        summary: "Готово",
      }),
    ]);
    expect(presented).toHaveLength(2);
    expect(presented[0]?.serviceHistory).toHaveLength(1);
    expect(presented.some((entry) => entry.completedServiceHistory)).toBe(
      false,
    );
  });
  it("не скрывает meaningful ошибку, UNSCOPED историю или неоднозначный FINAL", () => {
    const final = item("final", {
      kind: "agent",
      phase: "FINAL",
      state: "SUCCEEDED",
      summary: "Ответ",
    });
    const failure = item("failure", {
      eventType: "TURN_COMPLETED",
      messageKind: "FINAL_MESSAGE",
      state: "FAILED",
      summary: "Ошибка провайдера",
    });
    expect(presentRunTranscriptItems([failure, final])).toHaveLength(2);
    expect(
      presentRunTranscriptItems([
        item("old", { historical: true, execution: undefined }),
        final,
      ]),
    ).toHaveLength(2);
    const ambiguous = presentRunTranscriptItems([
      item("start"),
      final,
      { ...final, id: "another-final" },
    ]);
    expect(ambiguous).toHaveLength(3);
    expect(ambiguous.some((entry) => entry.completedServiceHistory)).toBe(
      false,
    );
    const meaningful = item("intermediate", {
      messageKind: "INTERMEDIATE_MESSAGE",
      summary: "Обнаружено две настройки",
    });
    expect(
      presentRunTranscriptItems([meaningful, final]).map(
        (entry) => entry.summary,
      ),
    ).toEqual([meaningful.summary, final.summary]);
  });

  it("распознаёт actual INTERMEDIATE progress по исходному коду даже после локализации caller", () => {
    const base = required(events[0]);
    const chain: PresentedRunEvent[] = [
      {
        ...base,
        ref: "evt_user",
        sequence: 1,
        type: "TURN_QUEUED",
        messageKind: "USER_MESSAGE",
        message: {
          source: { origin: "ORDINARY" as const },
          ref: "msg_user",
          phase: "USER",
          revision: 1,
          text: "Запрос",
        },
        summary: "Запрос",
        displaySummary: "Запрос",
        nodeState: "QUEUED",
      },
      {
        ...base,
        ref: "evt_start",
        sequence: 2,
        type: "TURN_STARTED",
        messageKind: "STATE",
        message: undefined,
        summary: "RUN_STARTED",
        displaySummary: "Выполняется",
        nodeState: "RUNNING",
      },
      {
        ...base,
        ref: "evt_schedule",
        sequence: 3,
        message: undefined,
        summary: "WORKLOAD_SCHEDULED",
        displaySummary: "Запуск передан исполнителю",
        nodeState: "RUNNING",
      },
      {
        ...base,
        ref: "evt_model",
        sequence: 4,
        message: undefined,
        summary: "MODEL_REQUEST_RUNNING",
        displaySummary: "Подготовка и выполнение запроса к модели",
        nodeState: "RUNNING",
      },
    ];
    const items = buildRunTranscriptItems(chain);
    expect(items.slice(2).map((entry) => entry.serviceProgressCode)).toEqual([
      "WORKLOAD_SCHEDULED",
      "MODEL_REQUEST_RUNNING",
    ]);
    const running = presentRunTranscriptItems(items);
    expect(running).toHaveLength(2);
    expect(running[1]?.serviceHistory).toHaveLength(3);
    expect(running.filter((entry) => entry.working)).toHaveLength(1);
    const failure: PresentedRunEvent = {
      ...base,
      ref: "evt_completed",
      sequence: 5,
      type: "TURN_COMPLETED",
      messageKind: "FINAL_MESSAGE",
      message: undefined,
      summary: "RUNTIME_PROVIDER_UNAVAILABLE",
      displaySummary: "Провайдер недоступен",
      nodeState: "FAILED",
    };
    const terminal = presentRunTranscriptItems(
      buildRunTranscriptItems([...chain, failure]),
    );
    expect(terminal).toHaveLength(2);
    expect(terminal[1]).toMatchObject({
      summary: "Провайдер недоступен",
      working: false,
      state: "FAILED",
    });
    expect(terminal[1]?.serviceHistory).toHaveLength(4);
    const commentary = {
      ...base,
      ref: "evt_commentary",
      sequence: 6,
      summary: "MODEL_REQUEST_RUNNING",
      displaySummary: "ignored",
      nodeState: "RUNNING" as const,
    };
    expect(
      presentRunTranscriptItems(buildRunTranscriptItems([commentary]))[0],
    ).toMatchObject({
      phase: "COMMENTARY",
      summary: "Собираю подтверждённые факты",
    });
    expect(
      presentRunTranscriptItems(buildRunTranscriptItems([commentary]))[0]
        ?.serviceHistory,
    ).toBeUndefined();
  });
});

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
  activitySummary: "Аналитик собирает подтверждённые факты",
  state: "RUNNING",
  source: "CONTROL_CENTER",
  initiator: { ref: "usr_example", displayName: "Владелец" },
  attempt: 1,
  graphRevision: 1,
  lastEventSequence: 2,
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

const events: PresentedRunEvent[] = [
  {
    ref: "evt_started",
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
      text: "Собираю подтверждённые факты",
    },
    runRef: run.ref,
    sequence: 2,
    type: "TURN_PROGRESS",
    messageKind: "INTERMEDIATE_MESSAGE",
    nodeRef: node.ref,
    summary: "ignored",
    displaySummary: "Собираю подтверждённые факты",
    occurredAt: "2026-08-28T08:00:02Z",
    graphRevision: 1,
    run: {
      ref: run.ref,
      version: 1,
      state: "RUNNING",
      graphRevision: 1,
      lastEventSequence: 2,
      usage: run.usage,
      artifactRefs: [],
      gateRefs: [],
      nextActions: [],
    },
  },
];

function required<T>(value: T | undefined): T {
  if (value === undefined) throw new Error("Required test fixture is missing");
  return value;
}

describe("terminal receipt раньше terminal RunEvent", () => {
  const user: AssistantTurn = {
    source: { origin: "ORDINARY" as const },
    ref: "trn_example",
    sequence: 1,
    role: "USER",
    content: "Запрос",
    state: "COMPLETED",
    runRef: run.ref,
    createdAt: run.createdAt,
  };
  const receipt: AssistantTurn = {
    ...user,
    ref: "trn_result",
    sequence: 2,
    role: "ASSISTANT",
    state: "FAILED",
    runVersion: 2,
  };
  const conversation: AssistantConversation = {
    ref: "cnv_example",
    version: 2,
    title: "Диалог",
    state: "ACTIVE",
    assistantScope: "SYSTEM",
    assistantRef: run.target.ref,
    titleSource: "SERVER_DEFAULT",
    titleRevision: 1,
    projectRef: run.projectRef,
    context: {
      route: "/",
      entityKind: "",
      entityRef: "",
      entityName: "",
      allowedOperations: [],
    },
    turns: [user, receipt],
    updatedAt: run.createdAt,
  };
  const ownedRun: Run = {
    ...run,
    source: "SYSTEM_ASSISTANT",
    target: { ...run.target, type: "SYSTEM_ASSISTANT" },
    assistantPin: {
      scope: "SYSTEM",
      organizationRef: "org_example",
      conversationRef: conversation.ref,
      assistantRef: run.target.ref,
      projectRef: run.projectRef,
    },
  };
  const boundNode = {
    ...node,
    agentRef: ownedRun.target.ref,
    turnRef: user.ref,
  };
  const progress = {
    ...required(events[0]),
    message: undefined,
    messageKind: "INTERMEDIATE_MESSAGE" as const,
    summary: "MODEL_REQUEST_RUNNING",
    displaySummary: "Подготовка запроса",
    nodeState: "RUNNING" as const,
  };
  const scopes = (
    changedConversation = conversation,
    changedRun = ownedRun,
    changedNode = boundNode,
    changedEvent = progress,
    organizationRef: string | undefined = "org_example",
  ) =>
    assistantTerminalTranscriptScopes(
      changedConversation,
      organizationRef,
      changedRun,
      [changedNode],
      [changedEvent],
    );

  it("скрывает только пустую successful ASSISTANT квитанцию с exact persisted owner и fresh terminal version", () => {
    const emptyReceipt = {
      ...receipt,
      content: " \n",
      state: "COMPLETED" as const,
    };
    const ownedConversation = { ...conversation, turns: [user, emptyReceipt] };
    const checkEmpty = (
      turn = emptyReceipt,
      currentConversation = ownedConversation,
      currentRun: Run | undefined = ownedRun,
      currentNode = boundNode,
      currentEvents: RunEvent[] = [progress],
      organizationRef: string | undefined = "org_example",
    ) =>
      assistantTurnIsEmptyTerminalReceipt(
        turn,
        currentConversation,
        organizationRef,
        currentRun,
        [currentNode],
        currentEvents,
      );
    expect(checkEmpty()).toBe(true);
    expect(
      assistantTurnIsEmptyTerminalReceipt(
        emptyReceipt,
        ownedConversation,
        "org_example",
        undefined,
        [boundNode],
        [progress],
      ),
    ).toBe(false);
    expect(
      checkEmpty(emptyReceipt, ownedConversation, ownedRun, boundNode, []),
    ).toBe(false);
    expect(
      checkEmpty(
        emptyReceipt,
        ownedConversation,
        ownedRun,
        boundNode,
        [progress],
        "foreign_org",
      ),
    ).toBe(false);
    expect(checkEmpty({ ...emptyReceipt, content: "Готов полный ответ" })).toBe(
      false,
    );
    expect(checkEmpty({ ...emptyReceipt, runVersion: 0 })).toBe(false);
    expect(checkEmpty({ ...emptyReceipt, runRef: "run_other" })).toBe(false);
    expect(
      checkEmpty(emptyReceipt, ownedConversation, ownedRun, {
        ...boundNode,
        turnRef: "trn_other",
      }),
    ).toBe(false);
    for (const state of ["RUNNING", "FAILED", "CANCELLED"] as const)
      expect(
        assistantTurnIsEmptyTerminalReceipt(
          { ...emptyReceipt, state },
          { ...ownedConversation, turns: [user, { ...emptyReceipt, state }] },
          "org_example",
          ownedRun,
          [boundNode],
          [progress],
        ),
      ).toBe(false);
  });

  it("убирает только exact машинную failed квитанцию до запуска, сохраняя другие ответы и события", () => {
    const message = "Входные данные исполнения недопустимы";
    const failedRun: Run = {
      ...ownedRun,
      state: "FAILED",
      safeErrorCode: "RUNTIME_INPUT_INVALID",
      safeErrorMessage: message,
    };
    const failedReceipt: AssistantTurn = {
      ...receipt,
      state: "FAILED",
      content: message,
    };
    const failedConversation = {
      ...conversation,
      turns: [user, failedReceipt],
    };
    const failedNode: RunNode = { ...boundNode, state: "FAILED" };
    const failedEvent: RunEvent = {
      ...progress,
      type: "NODE_STATE_CHANGED",
      runState: "FAILED",
      nodeState: "FAILED",
    };
    const check = (
      turn = failedReceipt,
      currentRun = failedRun,
      currentEvent = failedEvent,
      currentNode = failedNode,
      org: string | undefined = "org_example",
      currentConversation = failedConversation,
    ) =>
      assistantTurnIsDuplicateFailureReceipt(
        turn,
        currentConversation,
        org,
        currentRun,
        [currentNode],
        [currentEvent],
      );
    expect(check()).toBe(true);
    expect(
      check({ ...failedReceipt, content: "Есть дополнительная причина" }),
    ).toBe(false);
    expect(check({ ...failedReceipt, runVersion: 0 })).toBe(false);
    expect(check({ ...failedReceipt, role: "SYSTEM_RECEIPT" })).toBe(false);
    expect(
      check(failedReceipt, {
        ...failedRun,
        safeErrorCode: "PROVIDER_UNAVAILABLE",
      }),
    ).toBe(false);
    expect(
      check(failedReceipt, { ...failedRun, safeErrorMessage: undefined }),
    ).toBe(false);
    expect(check(failedReceipt, { ...failedRun, state: "RUNNING" })).toBe(
      false,
    );
    expect(
      check(failedReceipt, failedRun, {
        ...failedEvent,
        type: "TURN_PROGRESS",
      }),
    ).toBe(false);
    expect(
      check(failedReceipt, failedRun, { ...failedEvent, execution: undefined }),
    ).toBe(false);
    expect(
      check(failedReceipt, failedRun, { ...failedEvent, nodeState: "RUNNING" }),
    ).toBe(false);
    expect(
      check(failedReceipt, failedRun, failedEvent, {
        ...failedNode,
        state: "CANCELLED",
      }),
    ).toBe(false);
    expect(
      check(failedReceipt, failedRun, failedEvent, failedNode, "foreign_org"),
    ).toBe(false);
    expect(
      check(failedReceipt, failedRun, failedEvent, failedNode, "org_example", {
        ...failedConversation,
        ref: "cnv_foreign",
      }),
    ).toBe(false);
    const foreignExecution = {
      ...required(failedEvent.execution),
      sessionRef: "ses_foreign",
    };
    expect(
      check(failedReceipt, failedRun, {
        ...failedEvent,
        execution: foreignExecution,
      }),
    ).toBe(false);
  });

  it.each(["SYSTEM", "PROJECT"] as const)(
    "terminal %s receipt закрывает только exact pending progress",
    (scope) => {
      const currentConversation = {
        ...conversation,
        assistantScope: scope,
        assistantProfileRef: scope === "PROJECT" ? "asstp_example" : undefined,
      };
      const currentRun = {
        ...ownedRun,
        assistantPin: {
          ...required(ownedRun.assistantPin),
          scope,
          profileRef: currentConversation.assistantProfileRef,
        },
      };
      const closed = scopes(currentConversation, currentRun);
      expect(closed).toHaveLength(1);
      const items = buildRunTranscriptItems([progress]);
      expect(activeTranscriptItemId(items)).not.toBeNull();
      expect(activeTranscriptItemId(items, closed)).toBeNull();
      expect(
        presentRunTranscriptItems(
          items,
          activeTranscriptItemId(items, closed),
        )[0]?.working,
      ).toBe(false);
      const other = {
        ...progress,
        ref: "evt_parallel",
        runRef: "run_parallel",
        execution: {
          ...required(progress.execution),
          runRef: "run_parallel",
          turnRef: "trn_parallel",
          turnNumber: 2,
        },
      };
      const parallelItems = buildRunTranscriptItems([progress, other]);
      expect(activeTranscriptItemId(parallelItems, closed)).toBe(
        parallelItems[1]?.id,
      );
    },
  );

  it.each([
    "runRef",
    "nodeRef",
    "sessionRef",
    "turnRef",
    "turnNumber",
    "attempt",
  ] as const)("не подавляет другую execution.%s", (key) => {
    const execution = required(progress.execution);
    expect(
      scopes(conversation, ownedRun, boundNode, {
        ...progress,
        execution: {
          ...execution,
          [key]: typeof execution[key] === "number" ? 2 : "foreign_ref",
        },
      }),
    ).toEqual([]);
  });

  it("не suppress при stale/неполном receipt, foreign owner/pin/node и UNSCOPED истории", () => {
    expect(
      scopes({ ...conversation, turns: [user, { ...receipt, runVersion: 0 }] }),
    ).toEqual([]);
    expect(
      scopes({
        ...conversation,
        turns: [user, { ...receipt, runVersion: undefined }],
      }),
    ).toEqual([]);
    expect(
      scopes({
        ...conversation,
        turns: [user, { ...receipt, state: "RUNNING" }],
      }),
    ).toEqual([]);
    expect(
      scopes({
        ...conversation,
        turns: [user, { ...receipt, role: "SYSTEM_RECEIPT" }],
      }),
    ).toEqual([]);
    expect(
      scopes({
        ...conversation,
        turns: [user, { ...user, ref: "trn_duplicate" }, receipt],
      }),
    ).toEqual([]);
    expect(scopes({ ...conversation, ref: "cnv_foreign" })).toEqual([]);
    expect(scopes({ ...conversation, assistantRef: "agt_foreign" })).toEqual(
      [],
    );
    expect(
      scopes(conversation, ownedRun, { ...boundNode, attempt: 2 }),
    ).toEqual([]);
    expect(
      scopes(conversation, ownedRun, boundNode, {
        ...progress,
        execution: undefined,
      }),
    ).toEqual([]);
    expect(
      scopes(conversation, ownedRun, boundNode, progress, "org_foreign"),
    ).toEqual([]);
    expect(
      assistantTerminalTranscriptScopes(
        conversation,
        undefined,
        ownedRun,
        [boundNode],
        [progress],
      ),
    ).toEqual([]);
    expect(scopes(conversation, { ...ownedRun, version: 3 })).toEqual([]);
  });

  it("авторитетный terminal run закрывает exact ход без будущего receipt", () => {
    expect(
      scopes(
        { ...conversation, turns: [user] },
        { ...ownedRun, state: "FAILED" },
      ),
    ).toHaveLength(1);
  });
  it("terminal owner graph закрывает только bound node до terminal run/event", () => {
    expect(
      scopes({ ...conversation, turns: [user] }, ownedRun, {
        ...boundNode,
        state: "FAILED",
      }),
    ).toHaveLength(1);
    expect(
      scopes({ ...conversation, turns: [user] }, ownedRun, {
        ...boundNode,
        state: "FAILED",
        turnRef: "trn_foreign",
      }),
    ).toEqual([]);
  });

  it.each(["SYSTEM", "PROJECT"] as const)(
    "exact %s progress/COMMENTARY/tool заменяет working fallback, USER не заменяет",
    (scope) => {
      const pending: AssistantConversation = {
        ...conversation,
        assistantScope: scope,
        assistantProfileRef: scope === "PROJECT" ? "asstp_example" : undefined,
        turns: [{ ...user, state: "RUNNING", runVersion: ownedRun.version }],
      };
      const currentRun: Run = {
        ...ownedRun,
        assistantPin: {
          ...required(ownedRun.assistantPin),
          scope,
          profileRef: pending.assistantProfileRef,
        },
      };
      const replaces = (entries: RunEvent[]) =>
        assistantTranscriptReplacesWorkingFallback(
          pending,
          "org_example",
          currentRun,
          [boundNode],
          entries,
        );
      expect(replaces([])).toBe(false);
      expect(
        replaces([
          {
            ...progress,
            message: {
              source: { origin: "ORDINARY" as const },
              ref: "msg_user",
              phase: "USER",
              revision: 1,
              text: "Запрос",
            },
            messageKind: "USER_MESSAGE",
          },
        ]),
      ).toBe(false);
      expect(replaces([progress])).toBe(true);
      expect(
        replaces([
          {
            ...progress,
            message: {
              source: { origin: "ORDINARY" as const },
              ref: "msg_comment",
              phase: "COMMENTARY",
              revision: 1,
              text: "Проверяю",
            },
          },
        ]),
      ).toBe(true);
      expect(
        replaces([{ ...progress, type: "TURN_STARTED", messageKind: "STATE" }]),
      ).toBe(true);
      expect(
        replaces([
          {
            ...progress,
            type: "TOOL_CALL_RECORDED",
            messageKind: "TOOL_CALL",
            toolCall: {
              ref: "call_example",
              tool: "CODEX_SHELL",
              state: "RUNNING",
              revision: 1,
              durationMs: 0,
              safeParameters: {},
              safeResult: "",
              auditRef: "audit_example",
            },
          },
        ]),
      ).toBe(true);
      expect(
        replaces([
          {
            ...progress,
            message: {
              source: { origin: "ORDINARY" as const },
              ref: "msg_final",
              phase: "FINAL",
              revision: 1,
              text: "Готово",
            },
            nodeState: "SUCCEEDED",
          },
        ]),
      ).toBe(true);
    },
  );

  it("exact progress активного USER заменяет fallback после queued USER и позднего старого ответа", () => {
    const pending: AssistantConversation = {
      ...conversation,
      turns: [
        { ...user, runVersion: ownedRun.version, state: "RUNNING" },
        {
          ...user,
          ref: "trn_queued",
          sequence: user.sequence + 1,
          runRef: "run_queued",
          state: "QUEUED",
        },
        {
          ...user,
          ref: "trn_old_reply",
          sequence: user.sequence + 2,
          role: "ASSISTANT",
          runRef: "run_previous",
          state: "COMPLETED",
        },
      ],
    };
    const check = (candidate: AssistantConversation, entries: RunEvent[]) =>
      assistantTranscriptReplacesWorkingFallback(
        candidate,
        "org_example",
        ownedRun,
        [boundNode],
        entries,
      );
    expect(check(pending, [progress])).toBe(true);
    expect(
      check({ ...pending, turns: [...pending.turns].reverse() }, [progress]),
    ).toBe(true);
    expect(check(pending, [])).toBe(false);
    expect(check(pending, [{ ...progress, runRef: "run_previous" }])).toBe(
      false,
    );
    expect(check({ ...pending, state: "ARCHIVED" }, [progress])).toBe(false);
    expect(
      check(
        {
          ...pending,
          turns: [...pending.turns, { ...user, ref: "trn_duplicate" }],
        },
        [progress],
      ),
    ).toBe(false);
  });

  it("чужой/старый/unsigned progress и неподтверждённый owner read не suppress fallback нового хода", () => {
    const pending: AssistantConversation = {
      ...conversation,
      turns: [{ ...user, runVersion: ownedRun.version, state: "RUNNING" }],
    };
    const check = (
      candidate = pending,
      currentRun = ownedRun,
      currentNode = boundNode,
      entries: RunEvent[] = [progress],
      organizationRef: string | undefined = "org_example",
    ) =>
      assistantTranscriptReplacesWorkingFallback(
        candidate,
        organizationRef,
        currentRun,
        [currentNode],
        entries,
      );
    for (const key of [
      "runRef",
      "nodeRef",
      "sessionRef",
      "turnRef",
      "turnNumber",
      "attempt",
    ] as const) {
      const execution = required(progress.execution);
      expect(
        check(pending, ownedRun, boundNode, [
          {
            ...progress,
            execution: {
              ...execution,
              [key]: typeof execution[key] === "number" ? 2 : "foreign_ref",
            },
          },
        ]),
      ).toBe(false);
    }
    expect(
      check(pending, ownedRun, boundNode, [
        { ...progress, execution: undefined },
      ]),
    ).toBe(false);
    expect(
      check({
        ...pending,
        turns: [
          { ...user, runRef: "run_newturn", runVersion: 1, state: "RUNNING" },
        ],
      }),
    ).toBe(false);
    expect(
      check({
        ...pending,
        turns: [{ ...user, runVersion: 0, state: "RUNNING" }],
      }),
    ).toBe(false);
    expect(
      check({
        ...pending,
        turns: [{ ...user, runVersion: undefined, state: "RUNNING" }],
      }),
    ).toBe(false);
    expect(check(pending, { ...ownedRun, version: 3 })).toBe(false);
    expect(check({ ...pending, ref: "cnv_foreign" })).toBe(false);
    expect(check(pending, ownedRun, { ...boundNode, attempt: 2 })).toBe(false);
    expect(check(pending, ownedRun, boundNode, [progress], "org_foreign")).toBe(
      false,
    );
    expect(
      assistantTranscriptReplacesWorkingFallback(
        pending,
        undefined,
        ownedRun,
        [boundNode],
        [progress],
      ),
    ).toBe(false);
  });
});

describe("working fallback после tool", () => {
  it("между завершённым tool и следующим вызовом оставляет только нижний fallback", () => {
    const user: AssistantTurn = {
      source: { origin: "ORDINARY" as const },
      ref: "trn_example",
      sequence: 1,
      role: "USER",
      state: "RUNNING",
      runRef: run.ref,
      runVersion: run.version,
      content: "Запрос",
      createdAt: run.createdAt,
    };
    const pending: AssistantConversation = {
      ref: "cnv_example",
      version: 1,
      title: "Диалог",
      state: "ACTIVE",
      assistantScope: "SYSTEM",
      assistantRef: run.target.ref,
      projectRef: run.projectRef,
      titleSource: "SERVER_DEFAULT",
      titleRevision: 1,
      context: {
        route: "/",
        entityKind: "",
        entityRef: "",
        entityName: "",
        allowedOperations: [],
      },
      turns: [user],
      updatedAt: run.createdAt,
    };
    const ownedRun: Run = {
      ...run,
      source: "SYSTEM_ASSISTANT",
      target: { ...run.target, type: "SYSTEM_ASSISTANT" },
      assistantPin: {
        scope: "SYSTEM",
        organizationRef: "org_example",
        assistantRef: run.target.ref,
        conversationRef: pending.ref,
        projectRef: run.projectRef,
      },
    };
    const progress = {
      ...required(events[0]),
      message: undefined,
      nodeState: "RUNNING" as const,
    };
    const completed = {
      ...progress,
      ref: "evt_tool",
      sequence: 3,
      type: "TOOL_CALL_RECORDED" as const,
      messageKind: "TOOL_CALL" as const,
      toolCall: {
        ref: "call_exact",
        tool: "get_configuration_catalog",
        safeParameters: {},
        state: "SUCCEEDED" as const,
        revision: 2,
        durationMs: 10,
        safeResult: "get_configuration_catalog:completed",
        auditRef: "audit_exact",
      },
    };
    expect(
      assistantTranscriptReplacesWorkingFallback(
        pending,
        "org_example",
        ownedRun,
        [{ ...node, turnRef: user.ref }],
        [progress, completed],
      ),
    ).toBe(false);
    const items = buildRunTranscriptItems([progress, completed]);
    expect(activeTranscriptItemId(items)).toBeNull();
    expect(presentRunTranscriptItems(items).some((item) => item.working)).toBe(
      false,
    );
  });
});

describe("assistantTurnHasAuthoritativeActivity", () => {
  const user: AssistantTurn = {
    source: { origin: "ORDINARY" as const },
    ref: "trn_example",
    sequence: 1,
    role: "USER",
    content: "Задание",
    state: "COMPLETED",
    runRef: run.ref,
    createdAt: run.createdAt,
  };
  const receipt: AssistantTurn = {
    ...user,
    ref: "trn_result",
    sequence: 2,
    role: "ASSISTANT",
    state: "FAILED",
    content: "RUNTIME_PROVIDER_UNAVAILABLE",
  };
  const boundNode = { ...node, turnRef: user.ref, state: "FAILED" as const };
  const failure: PresentedRunEvent = {
    ...required(events[0]),
    message: undefined,
    type: "TURN_COMPLETED",
    messageKind: "FINAL_MESSAGE",
    summary: receipt.content,
    displaySummary: receipt.content,
    nodeState: "FAILED",
    runState: "FAILED",
  };
  const check = (
    event = failure,
    currentRun: Run | undefined = run,
    currentNode: RunNode = boundNode,
    turns = [user, receipt],
    turn = receipt,
  ) =>
    assistantTurnHasAuthoritativeActivity(
      turn,
      turns,
      currentRun,
      [currentNode],
      [event],
    );

  it("заменяет terminal summary exact failure без догадки по receipt ref/sequence", () => {
    expect(check()).toBe(true);
    expect(buildRunTranscriptItems([failure])).toMatchObject([
      { historical: false, state: "FAILED", summary: receipt.content },
    ]);
  });
  it("после rejoin убирает только соответствующий fallback и сохраняет старую историю", () => {
    expect(
      assistantTurnHasAuthoritativeActivity(
        receipt,
        [user, receipt],
        run,
        [boundNode],
        [],
      ),
    ).toBe(false);
    expect(check()).toBe(true);
    expect(
      check(failure, run, boundNode, [user, receipt], {
        ...receipt,
        runRef: "run_old",
      }),
    ).toBe(false);
  });
  it.each([
    "runRef",
    "nodeRef",
    "sessionRef",
    "turnRef",
    "turnNumber",
    "attempt",
  ] as const)("не подавляет summary для другой execution.%s", (field) => {
    const execution = required(failure.execution);
    const changed = {
      ...execution,
      [field]: typeof execution[field] === "number" ? 2 : "foreign_ref",
    };
    expect(check({ ...failure, execution: changed })).toBe(false);
  });
  it("сохраняет unsigned history и summary при неполном owner read", () => {
    expect(check({ ...failure, execution: undefined })).toBe(false);
    expect(
      assistantTurnHasAuthoritativeActivity(
        receipt,
        [user, receipt],
        undefined,
        [],
        [failure],
      ),
    ).toBe(false);
    expect(check(failure, run, { ...boundNode, turnRef: undefined })).toBe(
      false,
    );
    expect(check(failure, run, boundNode, [receipt])).toBe(false);
    expect(
      check(failure, run, boundNode, [
        user,
        { ...user, ref: "trn_other" },
        receipt,
      ]),
    ).toBe(false);
  });
  it("сохраняет неизвестную attempt/node и отдельный SYSTEM_RECEIPT", () => {
    expect(check(failure, { ...run, attempt: 2 })).toBe(false);
    expect(check(failure, run, { ...boundNode, attempt: 2 })).toBe(false);
    expect(
      check(failure, run, boundNode, [user, receipt], {
        ...receipt,
        role: "SYSTEM_RECEIPT",
      }),
    ).toBe(false);
  });
  it("не выдаёт progress/другой terminal state за ответ или отказ", () => {
    expect(
      check({
        ...failure,
        type: "TURN_PROGRESS",
        messageKind: "INTERMEDIATE_MESSAGE",
        nodeState: "RUNNING",
      }),
    ).toBe(false);
    expect(check({ ...failure, nodeState: "SUCCEEDED" })).toBe(false);
    expect(
      check(failure, run, boundNode, [user, receipt], {
        ...receipt,
        state: "COMPLETED",
      }),
    ).toBe(false);
  });
  it("скрывает USER/FINAL fallback только по точному опубликованному сообщению", () => {
    const message = {
      source: { origin: "ORDINARY" as const },
      ref: "msg_exact",
      phase: "USER" as const,
      revision: 1,
      text: user.content,
    };
    expect(
      check({ ...failure, message }, run, boundNode, [user, receipt], user),
    ).toBe(true);
    expect(check(failure, run, boundNode, [user, receipt], user)).toBe(false);
    expect(check({ ...failure, message: { ...message, phase: "FINAL" } })).toBe(
      true,
    );
  });
});

describe("buildRunActivityItems", () => {
  describe("точное повторное подтверждение успешной интеграции", () => {
    function completionFixture(): [RunEvent, RunEvent] {
      const base = required(events[0]);
      const invocationRef = "inv_fixture123";
      return [
        {
          ...base,
          ref: "evt_tool_success",
          type: "TOOL_CALL_RECORDED",
          messageKind: "TOOL_CALL",
          message: undefined,
          toolCall: {
            ref: "tcl_fixture123",
            tool: "context7_resolve_library_id",
            state: "SUCCEEDED",
            revision: 2,
            durationMs: 10,
            safeParameters: {},
            safeResult: JSON.stringify({
              version: 1,
              invocationRef,
              state: "SUCCEEDED",
              inputSHA256: "a".repeat(64),
            }),
            auditRef: "aud_fixture123",
          },
        },
        {
          ...base,
          ref: "evt_integration_success",
          sequence: 3,
          type: "TURN_PROGRESS",
          messageKind: "INTERMEDIATE_MESSAGE",
          message: undefined,
          summary: "i18n:INTEGRATION_ACTION_SUCCEEDED",
          integrationInvocationRef: invocationRef,
        },
      ];
    }

    function show(input: readonly RunEvent[]) {
      return presentRunTranscriptItems(buildRunTranscriptItems(input));
    }

    it("оставляет один tool при same six-tuple и owner invocation pin, не меняя events", () => {
      const input = completionFixture();
      const original = JSON.stringify(input);
      const result = show(input);
      expect(result).toHaveLength(1);
      expect(result[0]?.toolCall?.state).toBe("SUCCEEDED");
      expect(JSON.stringify(input)).toBe(original);
      expect(show([...input].reverse())).toEqual(result);
    });

    it("не связывает receipt с presentation-переводом summary", () => {
      const [tool, completion] = completionFixture();
      const translated: PresentedRunEvent = {
        ...completion,
        displaySummary: "Действие интеграции выполнено успешно",
      };
      expect(show([tool, translated])).toHaveLength(1);
    });

    it.each([
      "Действие интеграции выполнено успешно",
      "Integration action completed successfully",
    ])("сохраняет owner pin при gateway summary %s", (summary) => {
      const [tool, completion] = completionFixture();
      expect(
        show([
          tool,
          {
            ...completion,
            summary,
          },
        ]),
      ).toHaveLength(1);
    });

    it("до terminal revision сохраняет progress, после rejoin убирает только повтор", () => {
      const [tool, completion] = completionFixture();
      const running: RunEvent = {
        ...tool,
        ref: "evt_tool_running",
        toolCall: {
          ...required(tool.toolCall),
          revision: 1,
          state: "RUNNING",
          safeResult: "",
        },
      };
      expect(show([running, completion])).toHaveLength(2);
      expect(
        show([running, completion, { ...tool, sequence: 4 }]),
      ).toHaveLength(1);
    });

    it.each([
      "runRef",
      "nodeRef",
      "sessionRef",
      "turnRef",
      "turnNumber",
      "attempt",
    ] as const)("сохраняет completion другого %s", (field) => {
      const [tool, completion] = completionFixture();
      const execution = required(completion.execution);
      expect(
        show([
          tool,
          {
            ...completion,
            execution: {
              ...execution,
              [field]:
                typeof execution[field] === "number"
                  ? 2
                  : `${execution[field]}_foreign`,
            },
          },
        ]),
      ).toHaveLength(2);
    });

    it("не скрывает unbound, unknown, errors, meaningful или опубликованные messages", () => {
      const [tool, completion] = completionFixture();
      const variants: RunEvent[] = [
        { ...completion, integrationInvocationRef: undefined },
        { ...completion, integrationInvocationRef: "inv_otherfixture" },
        { ...completion, integrationInvocationRef: "inv_short" },
        { ...completion, execution: undefined },
        { ...completion, runRef: "run_foreignfixture" },
        { ...completion, nodeRef: "nod_foreignfixture" },
        { ...completion, artifactRef: "art_fixture123" },
        { ...completion, run: { ...completion.run, version: 0 } },
        {
          ...completion,
          run: { ...completion.run, ref: "run_foreignfixture" },
        },
        {
          ...completion,
          summary: "i18n:INTEGRATION_ACTION_SUCCEEDED\nЕсть пояснение",
          integrationInvocationRef: undefined,
        },
        { ...completion, progress: "Важные результаты" },
        { ...completion, type: "TURN_STARTED" },
        { ...completion, messageKind: "STATE" },
        {
          ...completion,
          message: {
            source: { origin: "ORDINARY" as const },
            ref: "msg_fixture123",
            phase: "COMMENTARY",
            revision: 1,
            text: "i18n:INTEGRATION_ACTION_SUCCEEDED",
          },
        },
      ];
      for (const variant of variants)
        expect(show([tool, variant])).toHaveLength(2);
    });

    it.each([
      "i18n:INTEGRATION_ACTION_FAILED",
      "i18n:INTEGRATION_ACTION_OUTCOME_UNKNOWN",
      "Действие интеграции не выполнено",
      "Исход действия интеграции неизвестен",
    ])("сохраняет terminal non-success outcome %s", (summary) => {
      const [tool, completion] = completionFixture();
      const failed: RunEvent = {
        ...tool,
        toolCall: {
          ...required(tool.toolCall),
          state: "FAILED",
          safeResult: "INTEGRATION_ACTION_FAILED",
        },
      };
      expect(show([failed, { ...completion, summary }])).toHaveLength(2);
    });

    it("не использует unknown, stale или противоречивую tool receipt", () => {
      const [tool, completion] = completionFixture();
      const call = required(tool.toolCall);
      const variants: RunEvent[] = [
        { ...tool, type: "TURN_PROGRESS" },
        { ...tool, messageKind: "INTERMEDIATE_MESSAGE" },
        { ...tool, runRef: "run_foreignfixture" },
        { ...tool, nodeRef: "nod_foreignfixture" },
        { ...tool, run: { ...tool.run, version: 0 } },
        { ...tool, toolCall: { ...call, revision: 1 } },
        { ...tool, toolCall: { ...call, state: "FAILED" } },
        { ...tool, toolCall: { ...call, tool: "unknown_tool" } },
        { ...tool, toolCall: { ...call, safeResult: `${call.safeResult} ` } },
      ];
      for (const variant of variants)
        expect(show([variant, completion])).toHaveLength(2);
    });
  });

  it("сохраняет полный USER текст больше 64 KiB в авторитетном бюджете 100000 символов", () => {
    const base = required(events[0]);
    const text = "я".repeat(40000);
    const user = {
      ...base,
      message: {
        source: { origin: "ORDINARY" as const },
        ref: "msg_user_long",
        phase: "USER" as const,
        revision: 1,
        text,
      },
    };
    expect(new TextEncoder().encode(text).length).toBeGreaterThan(65536);
    const items = buildRunActivityItems(
      run,
      [node],
      [user],
      "Короткий summary",
    );
    expect(items).toHaveLength(1);
    expect(items[0]).toMatchObject({
      phase: "USER",
      summary: text,
      historical: false,
    });
    expect(
      publishedRunMessage({
        ...user,
        message: { ...user.message, text: "😀".repeat(100000) },
      })?.text,
    ).toHaveLength(200000);
    expect(
      publishedRunMessage({
        ...user,
        message: { ...user.message, text: "x".repeat(100001) },
      }),
    ).toBeUndefined();
  });

  it.each(["COMMENTARY", "FINAL"] as const)(
    "закрыто отклоняет %s больше 64 KiB без summary fallback",
    (phase) => {
      const base = required(events[0]);
      const event = {
        ...base,
        summary: "Не использовать fallback",
        message: {
          source: { origin: "ORDINARY" as const },
          ref: "msg_oversized",
          phase,
          revision: 1,
          text: "я".repeat(33000),
        },
      };
      expect(publishedRunMessage(event)).toBeUndefined();
      const [item] = buildRunTranscriptItems([event]);
      expect(item?.phase).toBeUndefined();
      expect(item?.summary).toBeUndefined();
    },
  );
  it("разрешает автопрокрутку только возле конца, но не при чтении истории", () => {
    expect(
      isTranscriptNearBottom({
        scrollHeight: 2000,
        clientHeight: 400,
        scrollTop: 1504,
      }),
    ).toBe(true);
    expect(
      isTranscriptNearBottom({
        scrollHeight: 2000,
        clientHeight: 400,
        scrollTop: 1503,
      }),
    ).toBe(false);
    expect(
      isTranscriptNearBottom({
        scrollHeight: 300,
        clientHeight: 400,
        scrollTop: 0,
      }),
    ).toBe(true);
  });
  it("обновляет вызов на месте только по полной привязке и возрастанию revision", () => {
    const base = required(events[0]);
    const tool = {
      ref: "call_exact",
      tool: "project_files.search",
      safeParameters: {},
      revision: 1,
      state: "RUNNING" as const,
      durationMs: 0,
      safeResult: "",
      auditRef: "audit_exact",
    };
    const started: PresentedRunEvent = {
      ...base,
      ref: "evt_start",
      sequence: 2,
      message: undefined,
      toolCall: tool,
    };
    const completed: PresentedRunEvent = {
      ...started,
      ref: "evt_complete",
      sequence: 4,
      toolCall: {
        ...tool,
        revision: 2,
        state: "SUCCEEDED",
        safeResult: "Готово",
      },
    };
    const stale: PresentedRunEvent = {
      ...started,
      ref: "evt_stale",
      sequence: 5,
    };
    const result = buildRunTranscriptItems([completed, stale, started]);
    expect(result).toHaveLength(1);
    expect(result[0]).toMatchObject({
      sequence: 2,
      toolCall: { revision: 2, state: "SUCCEEDED", safeResult: "Готово" },
    });
    expect(buildRunTranscriptItems([completed, started])).toEqual(result);
  });

  it.each([
    "runRef",
    "nodeRef",
    "sessionRef",
    "turnRef",
    "turnNumber",
    "attempt",
  ] as const)("не объединяет одинаковый call ref с другой %s", (field) => {
    const base = required(events[0]);
    const toolCall = {
      ref: "call_same",
      tool: "project_files.search",
      safeParameters: {},
      revision: 1,
      state: "RUNNING" as const,
      durationMs: 0,
      safeResult: "",
      auditRef: "audit_same",
    };
    const execution = required(base.execution);
    const changed = {
      ...execution,
      [field]:
        typeof execution[field] === "number" ? 2 : `${execution[field]}_other`,
    };
    const result = buildRunTranscriptItems([
      { ...base, ref: "evt_one", message: undefined, toolCall },
      {
        ...base,
        ref: "evt_two",
        sequence: 3,
        execution: changed,
        message: undefined,
        toolCall: { ...toolCall, revision: 2, state: "FAILED" },
      },
    ]);
    expect(result).toHaveLength(2);
  });

  it("показывает полный текст сообщения и сортирует ход до sequence разных запусков", () => {
    const base = required(events[0]);
    const items = buildRunTranscriptItems([
      {
        ...base,
        ref: "evt_later_turn",
        sequence: 1,
        execution: { ...required(base.execution), turnNumber: 2 },
        message: {
          source: { origin: "ORDINARY" as const },
          ref: "msg_final",
          revision: 1,
          phase: "FINAL",
          text: "Полный ответ",
        },
      },
      {
        ...base,
        ref: "evt_first_turn",
        sequence: 90,
        summary: "Короткий summary",
        message: {
          source: { origin: "ORDINARY" as const },
          ref: "msg_progress",
          revision: 1,
          phase: "COMMENTARY",
          text: "Полное промежуточное сообщение",
        },
      },
    ]);
    expect(items.map((item) => item.summary)).toEqual([
      "Полное промежуточное сообщение",
      "Полный ответ",
    ]);
  });

  it("не назначает исторической записи текущий node/turn/attempt и не склеивает старый call", () => {
    const base = required(events[0]);
    const result = buildRunActivityItems(
      run,
      [node],
      [{ ...base, execution: undefined, message: undefined }],
    );
    expect(result[0]).toMatchObject({
      historical: true,
      kind: "system",
      nodeRef: undefined,
      execution: undefined,
    });
  });

  it("не дублирует общий terminal summary после опубликованного FINAL того же выполнения", () => {
    const base = required(events[0]);
    const result = buildRunTranscriptItems([
      {
        ...base,
        message: {
          source: { origin: "ORDINARY" as const },
          ref: "msg_final",
          phase: "FINAL",
          revision: 1,
          text: "Полный ответ",
        },
      },
      {
        ...base,
        ref: "evt_terminal",
        sequence: 4,
        type: "TURN_COMPLETED",
        messageKind: "FINAL_MESSAGE",
        message: undefined,
        summary: "Усечённый ответ",
      },
    ]);
    expect(result).toHaveLength(1);
    expect(result[0]?.summary).toBe("Полный ответ");
  });

  it("разделяет сообщение инициатора и сообщения ИИ-сотрудника", () => {
    const items = buildRunActivityItems(
      run,
      [node],
      events,
      "Проверь квартальный отчёт",
    );

    expect(items[0]).toMatchObject({
      kind: "initiator",
      actor: "Владелец",
      summary: "Проверь квартальный отчёт",
    });
    expect(items[1]).toMatchObject({
      kind: "agent",
      actor: "Аналитик продаж",
      nodeRef: node.ref,
    });
  });

  it("не выдумывает отсутствующие tool-call события", () => {
    const items = buildRunActivityItems(run, [node], events);

    expect(items).toHaveLength(1);
    expect(items.some((item) => item.kind === "tool")).toBe(false);
  });

  it("выделяет нормализованный tool call в самостоятельный блок", () => {
    const [firstEvent] = events;
    if (!firstEvent) {
      throw new Error("test event fixture is required");
    }
    const toolEvent: PresentedRunEvent = {
      ...firstEvent,
      ref: "evt_tool",
      sequence: 3,
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
        revision: 1,
        durationMs: 240,
        safeResult: "Найдено 4 фрагмента",
        auditRef: "evt_audit",
      },
    };

    const items = buildRunActivityItems(run, [node], [toolEvent]);

    expect(items[0]).toMatchObject({
      kind: "tool",
      actor: "Аналитик продаж",
      toolCall: { tool: "project_files.search" },
    });
  });

  it("сохраняет ссылку и безопасный descriptor файла из события", () => {
    const [firstEvent] = events;
    if (!firstEvent) throw new Error("test event fixture is required");
    const artifactEvent: PresentedRunEvent = {
      ...firstEvent,
      ref: "evt_artifact",
      type: "ARTIFACT_AVAILABLE",
      messageKind: "ARTIFACT",
      artifactRef: "art_report",
      artifact: {
        ref: "art_report",
        version: 1,
        projectRef: run.projectRef,
        runRef: run.ref,
        fileName: "report.md",
        mediaType: "text/markdown",
        sizeBytes: 512,
        digest: "sha256:example",
        scanState: "CLEAN",
        source: "AGENT_RESULT",
        revision: 1,
        lifecycleState: "ACTIVE",
        agentBindings: [],
        previewAvailable: true,
        createdAt: "2026-08-28T08:00:03Z",
        nextActions: ["DOWNLOAD"],
      },
    };

    const items = buildRunActivityItems(run, [node], [artifactEvent]);

    expect(items[0]).toMatchObject({
      artifactRef: "art_report",
      artifact: { fileName: "report.md", scanState: "CLEAN" },
    });
  });

  it("помещает tool call дочернего control node в timeline Session", () => {
    const toolNode: RunNode = {
      ...node,
      ref: "nod_tool",
      parentNodeRef: node.ref,
      type: "EXTERNAL_ACTION",
      displayName: "Поиск по файлам",
    };
    const [firstEvent] = events;
    if (!firstEvent) throw new Error("test event fixture is required");
    const toolEvent: PresentedRunEvent = {
      ...firstEvent,
      ref: "evt_tool_child",
      nodeRef: toolNode.ref,
      type: "TOOL_CALL_RECORDED",
      messageKind: "TOOL_CALL",
      toolCall: {
        ref: "trn_tool_child",
        tool: "project_files.search",
        safeParameters: {},
        state: "SUCCEEDED",
        revision: 1,
        durationMs: 80,
        safeResult: "",
        auditRef: "evt_audit_child",
      },
    };

    const items = buildRunActivityItems(run, [node, toolNode], [toolEvent]);

    expect(items).toHaveLength(1);
    expect(items[0]).toMatchObject({
      kind: "tool",
      nodeRef: node.ref,
      toolCall: { tool: "project_files.search" },
    });
  });

  it("оставляет решение владельца видимым в любом Session-фильтре", () => {
    const gateNode: RunNode = {
      ...node,
      ref: "nod_gate",
      parentNodeRef: node.ref,
      type: "HUMAN_GATE",
      displayName: "Проверить результат",
      state: "SUCCEEDED",
    };
    const [firstEvent] = events;
    if (!firstEvent) throw new Error("test event fixture is required");
    const gateEvent: PresentedRunEvent = {
      ...firstEvent,
      ref: "evt_gate_resolved",
      message: undefined,
      nodeRef: gateNode.ref,
      type: "OWNER_GATE_RESOLVED",
      messageKind: "OWNER_GATE",
      displaySummary: "Решение принято",
      actor: { kind: "USER", ref: "usr_owner", name: "Владелец" },
    };

    const items = buildRunActivityItems(run, [node, gateNode], [gateEvent]);

    expect(items[0]).toMatchObject({
      kind: "system",
      summary: "Решение принято",
      nodeRef: undefined,
    });
  });
});
