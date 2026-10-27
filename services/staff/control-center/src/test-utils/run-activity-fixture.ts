import type { Run, RunNode } from "@/shared/api/generated/openapi/types.gen";

// Создаёт отдельный синтетический запуск с различающимися сводкой, входом и sequence.
export function runActivityFixture(input: {
  activitySummary: string;
  lastEventSequence: number;
  inputSummary?: string;
}): Run {
  return {
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
    activitySummary: input.activitySummary,
    ...(input.inputSummary === undefined
      ? {}
      : { inputSummary: input.inputSummary }),
    state: "RUNNING",
    source: "CONTROL_CENTER",
    initiator: { ref: "usr_example", displayName: "Владелец" },
    attempt: 1,
    graphRevision: 1,
    lastEventSequence: input.lastEventSequence,
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
}

// Создаёт отдельный synthetic node для указанного запуска без связывания чужого состояния.
export function runActivityNodeFixture(runRef: string): RunNode {
  return {
    ref: "nod_agent",
    runRef,
    type: "AGENT_EXECUTION",
    state: "RUNNING",
    displayName: "Аналитик продаж",
    attempt: 1,
    artifactRefs: [],
    childRunRefs: [],
    createdAt: "2026-08-28T08:00:01Z",
    nextActions: [],
  };
}
