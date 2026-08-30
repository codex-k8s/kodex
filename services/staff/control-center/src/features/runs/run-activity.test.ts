import { describe, expect, it } from "vitest";

import {
  buildRunActivityItems,
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

describe("buildRunActivityItems", () => {
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
});
