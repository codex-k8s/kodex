import { createPinia, setActivePinia } from "pinia";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type {
  AssistantContextDescriptor,
  AssistantConversation,
  AssistantPlan,
} from "@/shared/api/generated/openapi/types.gen";

const createConversationMock = vi.hoisted(() => vi.fn());
const appendTurnMock = vi.hoisted(() => vi.fn());
const applyPlanDraftMock = vi.hoisted(() => vi.fn());
const readAssistantMock = vi.hoisted(() => vi.fn());
const readConversationsMock = vi.hoisted(() => vi.fn());

vi.mock("@/features/assistant/api", () => ({
  readAssistant: readAssistantMock,
  readConversations: readConversationsMock,
  createConversation: createConversationMock,
  appendTurn: appendTurnMock,
  renameConversation: vi.fn(),
  savePlanDraft: vi.fn(),
  validatePlanDraft: vi.fn(),
  applyPlanDraft: applyPlanDraftMock,
  rejectPlanDraft: vi.fn(),
}));

import { useAssistantStore } from "@/features/assistant/store";

const context: AssistantContextDescriptor = {
  route: "/projects/prj_sales",
  entityKind: "PROJECT",
  entityRef: "prj_sales",
  entityName: "Продажи",
  entityVersion: 1,
  allowedOperations: ["CREATE_AGENT"],
};

function plan(state: AssistantPlan["state"] = "VALID"): AssistantPlan {
  return {
    ref: "pln_sales",
    version: state === "STALE" ? 3 : 2,
    revision: 2,
    validatedRevision: 2,
    state,
    conversationRef: "cnv_sales",
    projectRef: "prj_sales",
    operations: [
      {
        ref: "op_sales",
        type: "CREATE_AGENT",
        action: "CREATE",
        title: "Создать сотрудника",
        summary: "Добавить координатора",
        target: { kind: "AGENT", name: "Координатор" },
        parameters: { name: "Координатор" },
        before: {},
        after: { state: "READY" },
        selected: true,
        permitted: true,
        validationProblems: [],
      },
    ],
    auditSummary: "Будет создан сотрудник",
    applied: false,
    contentDigest: "sha256:test",
    validationProblems: state === "STALE" ? ["operation-version-conflict"] : [],
    nextActions: state === "VALID" ? ["APPLY_PLAN"] : [],
  };
}

function conversation(value: AssistantPlan = plan()): AssistantConversation {
  return {
    ref: "cnv_sales",
    version: 2,
    title: "Настройка отдела продаж",
    titleSource: "AGENT_PROPOSED",
    titleRevision: 1,
    context,
    projectRef: "prj_sales",
    turns: [
      {
        ref: "trn_sales",
        sequence: 1,
        role: "ASSISTANT",
        content: "Подготовлен план",
        state: "COMPLETED",
        plan: value,
        createdAt: "2026-08-28T00:00:00Z",
      },
    ],
    updatedAt: "2026-08-28T00:00:00Z",
  };
}

function userTurn(state: "QUEUED" | "RUNNING" | "COMPLETED" | "FAILED") {
  return {
    ref: "trn_user",
    sequence: 2,
    role: "USER" as const,
    content: "Создай сотрудника",
    state,
    createdAt: "2026-08-28T00:01:00Z",
  };
}

describe("assistant workspace store", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    setActivePinia(createPinia());
    createConversationMock.mockReset();
    appendTurnMock.mockReset();
    applyPlanDraftMock.mockReset();
    readAssistantMock.mockReset();
    readConversationsMock.mockReset();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("создаёт server-context conversation перед первым сообщением", async () => {
    createConversationMock.mockResolvedValue(conversation());
    appendTurnMock.mockResolvedValue({
      ...conversation(),
      turns: [...conversation().turns, userTurn("QUEUED")],
    });
    const store = useAssistantStore();
    store.setContext(context, "prj_sales");

    await store.send("Создай сотрудника");

    expect(createConversationMock).toHaveBeenCalledWith(context, "prj_sales");
    expect(appendTurnMock).toHaveBeenCalledWith(
      "cnv_sales",
      "Создай сотрудника",
    );
    expect(store.selectedConversation?.turns).toHaveLength(2);
  });

  it("подхватывает terminal ответ без перезагрузки страницы", async () => {
    const initial = conversation();
    const queued = {
      ...initial,
      version: 3,
      turns: [...initial.turns, userTurn("QUEUED")],
    };
    const running = {
      ...queued,
      version: 4,
      turns: [...initial.turns, userTurn("RUNNING")],
    };
    const completed = {
      ...queued,
      version: 5,
      turns: [
        ...initial.turns,
        userTurn("COMPLETED"),
        {
          ref: "trn_result",
          sequence: 3,
          role: "ASSISTANT" as const,
          content: "План готов",
          state: "COMPLETED" as const,
          plan: plan(),
          createdAt: "2026-08-28T00:02:00Z",
        },
      ],
    };
    appendTurnMock.mockResolvedValue(queued);
    readConversationsMock
      .mockResolvedValueOnce([{ ...running, version: 2 }])
      .mockResolvedValueOnce([running])
      .mockResolvedValueOnce([completed]);
    const store = useAssistantStore();
    store.setContext(context, "prj_sales");
    store.conversations = [initial];
    store.selectedRef = initial.ref;

    await store.send("Создай сотрудника");
    await vi.advanceTimersByTimeAsync(1_000);
    expect(store.selectedConversation?.version).toBe(3);
    await vi.advanceTimersByTimeAsync(2_000);

    expect(readConversationsMock).toHaveBeenCalledTimes(3);
    expect(store.selectedConversation?.version).toBe(5);
    expect(store.selectedConversation?.turns.at(-1)?.content).toBe(
      "План готов",
    );
  });

  it("сохраняет conflict receipt и авторитетный STALE plan без частичного успеха", async () => {
    const stale = plan("STALE");
    applyPlanDraftMock.mockResolvedValue({
      conversation: conversation(stale),
      plan: stale,
      createdResourceRefs: [],
      receipt: {
        ref: "rcp_conflict",
        planRef: stale.ref,
        planRevision: stale.revision,
        outcome: "CONFLICT",
        operationReceipts: [],
        conflicts: [
          {
            operationRef: "op_sales",
            targetRef: "agt_existing",
            field: "version",
            expected: 2,
            actual: 3,
          },
        ],
        auditRefs: [],
        createdResourceRefs: [],
        createdAt: "2026-08-28T00:02:00Z",
      },
    });
    const store = useAssistantStore();
    store.conversations = [conversation()];
    store.selectedRef = "cnv_sales";

    const receipt = await store.apply(plan());

    expect(receipt.outcome).toBe("CONFLICT");
    expect(receipt.operationReceipts).toEqual([]);
    expect(receipt.createdResourceRefs).toEqual([]);
    expect(store.selectedConversation?.turns[0]?.plan?.state).toBe("STALE");
  });
});
