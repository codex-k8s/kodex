import { createPinia, setActivePinia } from "pinia";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type {
  AssistantContextDescriptor,
  AssistantConversation,
  AssistantPlan,
  SystemAssistant,
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

function systemAssistant(): SystemAssistant {
  return {
    ref: "ast_system_assistant",
    version: 7,
    name: "Kodex",
    system: true,
    removable: false,
    corePromptRevision: "core-v7",
    ownerInstructions: "",
    runtimeState: "READY",
    readinessSummary: "Готов",
    nextActions: ["ADD_TURN", "CREATE_CONVERSATION"],
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
    const created = { ...conversation(), turns: [] };
    createConversationMock.mockResolvedValue(created);
    appendTurnMock.mockResolvedValue({
      ...created,
      version: 3,
      turns: [userTurn("QUEUED")],
    });
    const store = useAssistantStore();
    store.setContext(context, "prj_sales");

    await store.send("Создай сотрудника");

    expect(createConversationMock).toHaveBeenCalledWith(context, "prj_sales");
    expect(appendTurnMock).toHaveBeenCalledWith(
      "cnv_sales",
      "Создай сотрудника",
    );
    expect(store.selectedConversation?.turns).toHaveLength(1);
  });

  it("создаёт и выбирает отдельный диалог до первого сообщения", async () => {
    const existing = conversation();
    const created = {
      ...conversation(),
      ref: "cnv_new_sales",
      sessionRef: "ses_new_sales",
      turns: [],
    };
    createConversationMock.mockResolvedValue(created);
    const store = useAssistantStore();
    store.setContext(context, "prj_sales");
    store.conversations = [existing];
    store.selectedRef = existing.ref;

    await store.startConversation();

    expect(createConversationMock).toHaveBeenCalledWith(context, "prj_sales");
    expect(store.selectedRef).toBe(created.ref);
    expect(store.selectedConversation?.turns).toEqual([]);
    expect(store.conversations).toHaveLength(2);
  });

  it("передаёт finalized AttachmentSet в сообщение помощнику", async () => {
    const initial = conversation();
    appendTurnMock.mockResolvedValue({
      ...initial,
      version: 3,
      turns: [userTurn("QUEUED")],
    });
    const store = useAssistantStore();
    store.setContext(context, "prj_sales");
    store.conversations = [initial];
    store.selectedRef = initial.ref;

    await store.send("Изучи вложения", "aset_contracts");

    expect(appendTurnMock).toHaveBeenCalledWith(
      "cnv_sales",
      "Изучи вложения",
      "aset_contracts",
    );
  });

  it("применяет terminal ответ из realtime snapshot без polling", async () => {
    const initial = conversation();
    const queued = {
      ...initial,
      version: 3,
      turns: [userTurn("QUEUED")],
    };
    const completed = {
      ...queued,
      version: 5,
      turns: [
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
    const store = useAssistantStore();
    store.setContext(context, "prj_sales");
    store.conversations = [initial];
    store.selectedRef = initial.ref;

    await store.send("Создай сотрудника");
    expect(
      store.selectedConversation?.turns.some((turn) => Boolean(turn.plan)),
    ).toBe(false);
    expect(vi.getTimerCount()).toBe(0);
    expect(readConversationsMock).not.toHaveBeenCalled();

    store.applyRealtimeSnapshot(systemAssistant(), [completed], "prj_sales");

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
