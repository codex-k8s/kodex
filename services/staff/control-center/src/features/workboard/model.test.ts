import { describe, expect, it } from "vitest";

import {
  collectAttention,
  decisionActionLayout,
  decisionHistory,
  decisionInbox,
  decisionUrgency,
  filterRuns,
  groupDecisionInbox,
  groupRuns,
  homeCapabilityCoverage,
  projectRuntimeEnvironments,
  projectSchedules,
  runExecutor,
} from "@/features/workboard/model";
import type {
  OwnerGate,
  Project,
  Run,
  RuntimeEnvironmentSet,
  Schedule,
} from "@/shared/api/generated/openapi/types.gen";
import { runPath } from "@/shared/routes";

const activitySummaryByState: Record<Run["state"], string> = {
  QUEUED: "Ожидает запуска",
  RUNNING: "Выполняется аналитиком продаж",
  WAITING_HUMAN: "Ожидает решения владельца",
  CANCELLING: "Отменяется по запросу владельца",
  SUCCEEDED: "Успешно завершён",
  FAILED: "Завершён с ошибкой",
  CANCELLED: "Отменён",
};

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
    activitySummary: activitySummaryByState[state],
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
    decisionConsequences: [
      {
        decision: "APPROVE",
        safeSummary: "Запуск продолжится",
        executesExternalEffect: false,
        terminalForRun: false,
      },
    ],
    openedAt: "2026-08-28T10:05:00Z",
    nextActions: ["RESOLVE_GATE"],
  };
}

const project: Project = {
  ref: "project_sales",
  version: 1,
  name: "Продажи",
  purpose: "Работа с клиентами",
  language: "ru",
  lifecycle: "ACTIVE",
  agentCount: 1,
  integrationState: "NONE",
  workflowCount: 1,
  activeRunCount: 1,
  pendingGateCount: 1,
  updatedAt: "2026-08-28T10:00:00Z",
  nextActions: [],
};

function schedule(
  ref: string,
  state: Schedule["state"],
  options: Partial<Schedule> = {},
): Schedule {
  return {
    input: {},
    ref,
    version: 1,
    projectRef: project.ref,
    name: ref,
    target: {
      type: "AGENT",
      ref: "agent_sales",
      displayName: "Аналитик",
      version: 1,
    },
    state,
    preset: "DAILY",
    timezone: "Europe/Saratov",
    sessionPolicy: "NEW_EACH_RUN",
    notificationPolicy: "CONTROL_CENTER_ONLY",
    dstGapPolicy: "SHIFT_FORWARD",
    dstFoldPolicy: "RUN_ONCE_EARLIEST",
    misfirePolicy: "COALESCE",
    overlapPolicy: "FORBID",
    automationText: "",
    promptInputs: {},
    nextActions: [],
    targetVersion: 2,
    targetDigest: "b".repeat(64),
    cronExpression: "0 9 * * *",
    currentRevision: {
      ref: `revision_${ref}`,
      revision: 1,
      digest: `digest_${ref}`,
      targetVersion: 2,
      targetDigest: "b".repeat(64),
      name: ref,
      target: {
        type: "AGENT",
        ref: "agent_sales",
        displayName: "Аналитик",
        version: 1,
      },
      preset: "DAILY",
      cronExpression: "0 9 * * *",
      timezone: "Europe/Saratov",
      input: {},
      sessionPolicy: "NEW_EACH_RUN",
      notificationPolicy: "CONTROL_CENTER_ONLY",
      dstGapPolicy: "SHIFT_FORWARD",
      dstFoldPolicy: "RUN_ONCE_EARLIEST",
      misfirePolicy: "COALESCE",
      overlapPolicy: "FORBID",
      automationText: "",
      promptInputs: {},
      createdAt: "2026-08-28T09:00:00Z",
    },
    ...options,
  };
}

function environment(
  ref: string,
  updatedAt: string,
  projectRef = project.ref,
): RuntimeEnvironmentSet {
  return {
    ref,
    version: 1,
    projectRef,
    name: ref,
    description: "Окружение",
    state: "ACTIVE",
    currentVersion: {} as RuntimeEnvironmentSet["currentVersion"],
    updatedAt,
    ready: true,
    readinessBlockers: [],
    nextActions: [],
  };
}

describe("workboard model", () => {
  it("явно отмечает источники Home, отсутствующие в Overview API", () => {
    expect(homeCapabilityCoverage()).toEqual([
      {
        key: "STOPPED_RUNS",
        availability: "UNAVAILABLE",
        reason: "NOT_IN_OVERVIEW_API",
      },
      {
        key: "PROVIDER_AUTH_EXPIRY",
        availability: "UNAVAILABLE",
        reason: "NOT_IN_OVERVIEW_API",
      },
      {
        key: "SESSION_CONTINUATION",
        availability: "UNAVAILABLE",
        reason: "NOT_IN_OVERVIEW_API",
      },
    ]);
  });

  it("выбирает автоматизации Проекта и поднимает требующие внимания", () => {
    const values = projectSchedules(
      [
        schedule("later", "ACTIVE", {
          nextRunAt: "2026-08-30T12:00:00Z",
        }),
        schedule("attention", "NEEDS_ATTENTION"),
        schedule("other", "ACTIVE", { projectRef: "project_other" }),
        schedule("sooner", "ACTIVE", {
          nextRunAt: "2026-08-30T10:00:00Z",
        }),
        schedule("archived", "ARCHIVED"),
        schedule("deleted", "DELETED"),
      ],
      project.ref,
    );

    expect(values.map((item) => item.ref)).toEqual([
      "attention",
      "sooner",
      "later",
      "archived",
      "deleted",
    ]);
  });

  it("показывает только окружения Проекта от недавно изменённых", () => {
    const values = projectRuntimeEnvironments(
      [
        environment("older", "2026-08-28T10:00:00Z"),
        environment("other", "2026-08-30T10:00:00Z", "project_other"),
        environment("newer", "2026-08-29T10:00:00Z"),
      ],
      project.ref,
    );

    expect(values.map((item) => item.ref)).toEqual(["newer", "older"]);
  });

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

  it("формирует decision inbox только из открытых решений нужного Проекта", () => {
    const current = gate("run_current");
    const closed = {
      ...gate("run_closed"),
      ref: "gate_closed",
      state: "APPROVED" as const,
    };
    const other = {
      ...gate("run_other"),
      ref: "gate_other",
      projectRef: "project_other",
    };

    const items = decisionInbox(
      [closed, other, current],
      [project],
      project.ref,
      new Date("2026-08-28T10:00:00Z"),
    );

    expect(items).toHaveLength(1);
    expect(items[0]?.project?.name).toBe("Продажи");
    expect(items[0]?.canResolve).toBe(true);
  });

  it("честно отмечает неполный или недоступный контекст решения", () => {
    const item = decisionInbox(
      [
        {
          ...gate("run_unavailable"),
          contextSummary: " ",
          consequencesSummary: "",
        },
      ],
      [project],
    )[0];

    expect(item).toMatchObject({
      hasQuestion: false,
      hasConsequences: false,
      canResolve: false,
    });
  });

  it("сортирует срочные решения до обычных", () => {
    const now = new Date("2026-08-28T10:00:00Z");
    const soon = {
      ...gate("run_soon"),
      ref: "gate_soon",
      expiresAt: "2026-08-28T12:00:00Z",
    };
    const normal = {
      ...gate("run_normal"),
      ref: "gate_normal",
      expiresAt: "2026-08-30T10:00:00Z",
    };

    expect(decisionUrgency(soon, now)).toBe("SOON");
    expect(
      decisionInbox([normal, soon], [project], undefined, now).map(
        (item) => item.gate.ref,
      ),
    ).toEqual(["gate_soon", "gate_normal"]);
  });

  it("связывает решение с авторитетным запуском и группирует по срочности и Проекту", () => {
    const currentRun = run("run_current", "WAITING_HUMAN", {
      title: "Согласование предложения",
    });
    const secondRun = run("run_second", "WAITING_HUMAN", {
      title: "Согласование договора",
    });
    const items = decisionInbox(
      [
        gate(currentRun.ref),
        {
          ...gate(secondRun.ref),
          ref: "gate_second",
        },
      ],
      [project],
      undefined,
      new Date("2026-08-28T10:00:00Z"),
      [currentRun, secondRun],
    );
    const groups = groupDecisionInbox(items);

    expect(items[0]?.run?.title).toBe("Согласование предложения");
    expect(groups).toHaveLength(1);
    expect(groups[0]).toMatchObject({
      project: { name: "Продажи" },
      items: [
        { gate: { ref: "gate_owner" } },
        { gate: { ref: "gate_second" } },
      ],
    });
  });

  it("выбирает ровно одно основное действие и сохраняет безопасный порядок остальных", () => {
    const actions = decisionActionLayout({
      ...gate("run_actions"),
      allowedDecisions: ["REJECT", "APPROVE", "REQUEST_CHANGES"],
    });

    expect(actions).toEqual({
      primary: "APPROVE",
      secondary: ["REQUEST_CHANGES", "REJECT"],
    });
    expect(
      decisionActionLayout({
        ...gate("run_without_approve"),
        allowedDecisions: ["REJECT", "REQUEST_CHANGES"],
      }),
    ).toEqual({
      primary: "REQUEST_CHANGES",
      secondary: ["REJECT"],
    });
  });

  it("отделяет закрытые Human Gates от ожидающих решения", () => {
    const approved = {
      ...gate("run_approved"),
      state: "APPROVED" as const,
      decision: "APPROVE" as const,
      decidedAt: "2026-08-28T11:00:00Z",
      nextActions: [],
    };

    expect(decisionInbox([approved], [project])).toHaveLength(0);
    expect(decisionHistory([approved], [project])).toMatchObject([
      {
        gate: { ref: "gate_owner", state: "APPROVED" },
        canResolve: false,
      },
    ]);
  });
});
