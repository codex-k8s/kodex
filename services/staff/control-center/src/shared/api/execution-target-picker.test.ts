import { beforeEach, describe, expect, it, vi } from "vitest";

import {
  createExecutionTargetPickerLoader,
  targetRefAfterTypeChange,
} from "@/shared/api/execution-target-picker";
import type { Agent, Workflow } from "@/shared/api/generated/openapi/types.gen";

const api = vi.hoisted(() => ({
  listAgents: vi.fn(),
  listWorkflows: vi.fn(),
}));

vi.mock("@/shared/api/generated/openapi/sdk.gen", () => api);
vi.mock("@/shared/api/client", () => ({
  requestSignal: () => new AbortController().signal,
}));

function response<T>(data: T) {
  return Promise.resolve({
    data,
    response: new Response(null, { status: 200 }),
  });
}

function agent(overrides: Partial<Agent> = {}): Agent {
  return {
    ref: "agent_sales",
    version: 1,
    projectRef: "project_sales",
    name: "Аналитик продаж",
    purpose: "Квалифицирует входящие обращения",
    roleDescription: "Аналитик",
    state: "READY",
    enabled: true,
    system: false,
    runtimeRef: "runtime_default",
    runtimeName: "Основная среда",
    runtimeReady: true,
    capabilities: [],
    integrations: [],
    knowledgeArtifactRefs: [],
    updatedAt: "2026-08-29T10:00:00Z",
    nextActions: ["LAUNCH"],
    ...overrides,
  };
}

function workflow(overrides: Partial<Workflow> = {}): Workflow {
  return {
    ref: "workflow_sales",
    launchReadiness: {
      allowedToSubmit: true,
      reason: "READY",
      workflowVersion: 4,
      operationalState: "UNKNOWN",
      contextDigest: "a".repeat(64),
    },
    version: 4,
    projectRef: "project_sales",
    name: "Квалификация обращения",
    purpose: "Проверяет обращение и готовит ответ",
    state: "PUBLISHED",
    cardSummary: {
      stageCount: 0,
      uniqueAgentCount: 0,
      parallelGroupCount: 0,
      hasHumanGate: false,
      activeRunCount: 0,
      pendingGateCount: 0,
    },
    inputFields: [],
    steps: [],
    validationMessages: [],
    updatedAt: "2026-08-29T10:00:00Z",
    nextActions: ["LAUNCH"],
    ...overrides,
  };
}

describe("execution target picker API", () => {
  beforeEach(() => vi.clearAllMocks());

  it("передаёт серверный поиск и cursor и пропускает страницу без доступных сотрудников", async () => {
    api.listAgents
      .mockReturnValueOnce(
        response({
          items: [agent({ ref: "agent_system", system: true })],
          nextPageToken: "agent-page-2",
        }),
      )
      .mockReturnValueOnce(
        response({
          items: [agent()],
          nextPageToken: "agent-page-3",
        }),
      );
    const loader = createExecutionTargetPickerLoader("project_sales", "AGENT");

    const page = await loader(
      "  продажи  ",
      "agent-page-1",
      new AbortController().signal,
    );

    expect(api.listAgents).toHaveBeenCalledTimes(2);
    expect(api.listAgents).toHaveBeenNthCalledWith(
      1,
      expect.objectContaining({
        path: { projectRef: "project_sales" },
        query: {
          pageSize: 40,
          pageToken: "agent-page-1",
          query: "продажи",
        },
      }),
    );
    expect(api.listAgents).toHaveBeenNthCalledWith(
      2,
      expect.objectContaining({
        query: {
          pageSize: 40,
          pageToken: "agent-page-2",
          query: "продажи",
        },
      }),
    );
    expect(page.items).toMatchObject([
      { ref: "agent_sales", title: "Аналитик продаж", targetType: "AGENT" },
    ]);
    expect(page.nextPageToken).toBe("agent-page-3");
  });

  it("возвращает только опубликованные запускаемые процессы со второй страницы", async () => {
    api.listWorkflows.mockReturnValueOnce(
      response({
        items: [
          workflow({ ref: "workflow_draft", state: "DRAFT" }),
          workflow(),
          workflow({ ref: "workflow_locked", nextActions: [] }),
        ],
        nextPageToken: "workflow-page-3",
      }),
    );
    const loader = createExecutionTargetPickerLoader(
      "project_sales",
      "WORKFLOW",
    );

    const page = await loader(
      "",
      "workflow-page-2",
      new AbortController().signal,
    );

    expect(api.listWorkflows).toHaveBeenCalledWith(
      expect.objectContaining({
        query: { pageSize: 40, pageToken: "workflow-page-2" },
      }),
    );
    expect(page.items).toMatchObject([
      {
        ref: "workflow_sales",
        title: "Квалификация обращения",
        targetType: "WORKFLOW",
      },
    ]);
    expect(page.nextPageToken).toBe("workflow-page-3");
  });

  it.each([
    ["AGENT", "AGENT", "agent_sales", "agent_sales"],
    ["AGENT", "WORKFLOW", "agent_sales", ""],
    ["WORKFLOW", "AGENT", "workflow_sales", ""],
  ] as const)(
    "при смене типа %s -> %s корректно сохраняет или очищает цель",
    (currentType, nextType, currentRef, expected) => {
      expect(targetRefAfterTypeChange(currentType, nextType, currentRef)).toBe(
        expected,
      );
    },
  );
});
