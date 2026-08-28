import { describe, expect, it } from "vitest";

import {
  collectAttention,
  filterRuns,
  groupRuns,
  runExecutor,
} from "@/features/workboard/model";
import type { OwnerGate, Run } from "@/shared/api/generated/openapi/types.gen";
import { runPath } from "@/shared/routes";

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
    inputArtifactRefs: [],
    artifactRefs: [],
    gateRefs: [],
    createdAt: "2026-08-28T10:00:00Z",
    nextActions: [],
    ...options,
  };
}

function gate(runRef: string): OwnerGate {
  return {
    ref: "gate_owner",
    version: 1,
    projectRef: "project_sales",
    runRef,
    nodeRef: "node_gate",
    title: "Подтвердить отправку",
    contextSummary: "Нужно решение владельца",
    consequencesSummary: "Запуск продолжится",
    requestedBy: { ref: "agent_sales", displayName: "Аналитик продаж" },
    state: "OPEN",
    allowedDecisions: ["APPROVE"],
    openedAt: "2026-08-28T10:05:00Z",
    artifactRefs: [],
    nextActions: ["RESOLVE_GATE"],
  };
}

describe("workboard model", () => {
  it("группирует состояния в четыре канонические колонки", () => {
    const grouped = groupRuns([
      run("queued", "QUEUED"),
      run("running", "RUNNING"),
      run("cancelling", "CANCELLING"),
      run("gate", "WAITING_HUMAN"),
      run("success", "SUCCEEDED"),
      run("failed", "FAILED"),
    ]);

    expect(grouped.QUEUED.map((item) => item.ref)).toEqual(["queued"]);
    expect(grouped.RUNNING.map((item) => item.ref)).toEqual([
      "running",
      "cancelling",
    ]);
    expect(grouped.WAITING_HUMAN.map((item) => item.ref)).toEqual(["gate"]);
    expect(grouped.TERMINAL.map((item) => item.ref)).toEqual([
      "success",
      "failed",
    ]);
  });

  it("фильтрует активные и terminal запуски без изменения исходного массива", () => {
    const source = [
      run("old", "SUCCEEDED", { createdAt: "2026-08-27T10:00:00Z" }),
      run("new", "RUNNING", { createdAt: "2026-08-28T10:00:00Z" }),
    ];

    expect(filterRuns(source, "ACTIVE").map((item) => item.ref)).toEqual([
      "new",
    ]);
    expect(filterRuns(source, "TERMINAL").map((item) => item.ref)).toEqual([
      "old",
    ]);
    expect(source.map((item) => item.ref)).toEqual(["old", "new"]);
  });

  it("не выдаёт workflow target за фактического исполнителя", () => {
    const workflowRun = run("workflow", "RUNNING", {
      target: {
        type: "WORKFLOW",
        ref: "workflow_sales",
        displayName: "Квалификация лида",
        version: 2,
      },
    });

    expect(runExecutor(run("agent", "RUNNING"))).toBe("Аналитик продаж");
    expect(runExecutor(workflowRun)).toBeUndefined();
  });

  it("объединяет открытые Human Gate и нерешённые инциденты", () => {
    const currentRun = run("attention", "WAITING_HUMAN", {
      incidents: [
        {
          ref: "incident_open",
          runRef: "attention",
          category: "RUNTIME",
          severity: "ERROR",
          state: "OPEN",
          safeSummary: "Runtime unavailable",
          safeNextStep: "Retry later",
          coreAffected: false,
          createdAt: "2026-08-28T10:06:00Z",
        },
        {
          ref: "incident_resolved",
          runRef: "attention",
          category: "RUNTIME",
          severity: "INFO",
          state: "RESOLVED",
          safeSummary: "Recovered",
          safeNextStep: "No action",
          coreAffected: false,
          createdAt: "2026-08-28T10:07:00Z",
        },
      ],
    });

    expect(
      collectAttention([currentRun], [gate(currentRun.ref)]).map(
        (item) => item.ref,
      ),
    ).toEqual(["incident_open", "gate_owner"]);
  });

  it("использует project-scoped ссылку только внутри Проекта", () => {
    expect(runPath("context", "project_sales")).toBe(
      "/projects/project_sales/runs/context",
    );
    expect(runPath("context")).toBe("/runs/context");
  });
});
