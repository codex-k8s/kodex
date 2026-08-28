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

const events: PresentedRunEvent[] = [
  {
    ref: "evt_started",
    runRef: run.ref,
    sequence: 2,
    type: "TURN_PROGRESS",
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

    expect(items).toHaveLength(2);
    expect(items.some((item) => "tool" in item)).toBe(false);
  });
});
