import { describe, expect, it } from "vitest";

import {
  homeFailedRuns,
  homeOpenGates,
  homeResumableSessions,
  prioritizeHomeProjects,
} from "@/features/home/model";
import type {
  OwnerGate,
  Project,
  Run,
} from "@/shared/api/generated/openapi/types.gen";

function run(
  ref: string,
  state: Run["state"],
  options: Partial<Run> = {},
): Run {
  return {
    ref,
    version: 1,
    projectRef: "project_sales",
    sessionRef: `session_${ref}`,
    rootRunRef: ref,
    target: {
      type: "AGENT",
      ref: "agent_sales",
      displayName: "Аналитик продаж",
      version: 1,
    },
    title: `Запуск ${ref}`,
    titleSource: "SERVER_DEFAULT",
    activitySummary: "Состояние запуска",
    state,
    source: "CONTROL_CENTER",
    initiator: { ref: "user_owner", displayName: "Владелец" },
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
    createdAt: "2026-08-29T10:00:00Z",
    nextActions: [],
    ...options,
  };
}

function gate(
  ref: string,
  state: OwnerGate["state"],
  options: Partial<OwnerGate> = {},
): OwnerGate {
  return {
    ref,
    version: 1,
    projectRef: "project_sales",
    runRef: `run_${ref}`,
    nodeRef: `node_${ref}`,
    title: `Решение ${ref}`,
    contextSummary: "Нужно решение владельца",
    consequencesSummary: "Запуск продолжится",
    requestedBy: { ref: "agent_sales", displayName: "Аналитик продаж" },
    state,
    allowedDecisions: ["APPROVE"],
    decisionConsequences: [],
    openedAt: "2026-08-29T10:00:00Z",
    nextActions: ["RESOLVE_GATE"],
    ...options,
  };
}

describe("home attention model", () => {
  it("поднимает Проекты с решениями и активной работой выше просто недавних", () => {
    const project = (
      ref: string,
      pendingGateCount: number,
      activeRunCount: number,
      updatedAt: string,
    ): Project => ({
      ref,
      version: 1,
      name: ref,
      purpose: ref,
      language: "ru",
      lifecycle: "ACTIVE",
      agentCount: 0,
      workflowCount: 0,
      activeRunCount,
      pendingGateCount,
      updatedAt,
      nextActions: [],
    });

    expect(
      prioritizeHomeProjects([
        project("recent", 0, 0, "2026-08-31T12:00:00Z"),
        project("running", 0, 2, "2026-08-30T12:00:00Z"),
        project("decision", 1, 0, "2026-08-29T12:00:00Z"),
      ]).map((item) => item.ref),
    ).toEqual(["decision", "running", "recent"]);
  });

  it("показывает только открытые решения и сортирует ближайший срок первым", () => {
    const result = homeOpenGates([
      gate("later", "OPEN", { expiresAt: "2026-08-31T10:00:00Z" }),
      gate("closed", "APPROVED"),
      gate("soon", "OPEN", { expiresAt: "2026-08-30T10:00:00Z" }),
    ]);

    expect(result.map((item) => item.ref)).toEqual(["soon", "later"]);
  });

  it("не считает обычную отмену ошибкой, но показывает подтвержденный аварийный stop", () => {
    const result = homeFailedRuns([
      run("failed", "FAILED", { finishedAt: "2026-08-29T11:00:00Z" }),
      run("cancelled", "CANCELLED"),
      run("timeout", "CANCELLED", {
        safeErrorCode: "RUN_TIMEOUT",
        nextActions: ["RETRY"],
        finishedAt: "2026-08-29T12:00:00Z",
      }),
      run("active", "RUNNING"),
    ]);

    expect(result.map((item) => item.ref)).toEqual(["timeout", "failed"]);
  });

  it("оставляет одну последнюю продолжимую работу на сессию и не дублирует ошибки", () => {
    const result = homeResumableSessions([
      run("old", "SUCCEEDED", {
        sessionRef: "session_shared",
        nextActions: ["ADD_TURN"],
        finishedAt: "2026-08-29T09:00:00Z",
      }),
      run("latest", "SUCCEEDED", {
        sessionRef: "session_shared",
        nextActions: ["ADD_TURN"],
        finishedAt: "2026-08-29T12:00:00Z",
      }),
      run("failed", "FAILED", {
        nextActions: ["ADD_TURN"],
        finishedAt: "2026-08-29T13:00:00Z",
      }),
      run("not-allowed", "SUCCEEDED"),
    ]);

    expect(result.map((item) => item.ref)).toEqual(["latest"]);
  });
});
