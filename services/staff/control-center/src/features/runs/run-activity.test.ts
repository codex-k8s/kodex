import { describe, expect, it } from "vitest";

import {
  buildRunActivityItems,
  buildRunTranscriptItems,
  isTranscriptNearBottom,
  publishedRunMessage,
  type PresentedRunEvent,
} from "@/features/runs/run-activity";
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

describe("buildRunActivityItems", () => {
  it("сохраняет полный USER текст больше 64 KiB в авторитетном бюджете 100000 символов", () => {
    const base = required(events[0]);
    const text = "я".repeat(40000);
    const user = {
      ...base,
      message: {
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
