import { createPinia, setActivePinia } from "pinia";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type {
  AssistantContextDescriptor,
  AssistantConversation,
  AssistantPlan,
  SystemAssistant,
  ListAssistantConversationsResponse,
} from "@/shared/api/generated/openapi/types.gen";
import { AppProblem } from "@/shared/api/problem";

const createConversationMock = vi.hoisted(() => vi.fn());
const appendTurnMock = vi.hoisted(() => vi.fn());
const archiveConversationMock = vi.hoisted(() => vi.fn());
const applyPlanDraftMock = vi.hoisted(() => vi.fn());
const readAssistantMock = vi.hoisted(() => vi.fn());
const readConversationsMock = vi.hoisted(() => vi.fn());

vi.mock("@/features/assistant/api", () => ({
  readAssistant: readAssistantMock,
  readConversations: readConversationsMock,
  createConversation: createConversationMock,
  appendTurn: appendTurnMock,
  archiveConversation: archiveConversationMock,
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
    state: "ACTIVE",
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

function deferred<T>(): {
  promise: Promise<T>;
  resolve: (value: T) => void;
} {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((next) => {
    resolve = next;
  });
  return { promise, resolve };
}

describe("assistant workspace store", () => {
  it("убирает прежнюю защищённую проекцию после отказа свежего чтения", async () => {
    readAssistantMock.mockResolvedValue(systemAssistant());
    readConversationsMock.mockResolvedValue({ items: [conversation()] });
    const store = useAssistantStore();
    await store.load(context, "prj_sales");
    expect(store.selectedConversation).toBeDefined();
    readConversationsMock.mockRejectedValueOnce(new Error("READ_FORBIDDEN"));
    await store.load(context, "prj_sales");
    expect(store.problem).toBeDefined();
    expect(store.conversations).toEqual([]);
    expect(store.selectedConversation).toBeUndefined();
    expect(store.assistant).toBeUndefined();
    expect(store.nextPageToken).toBeUndefined();
  });

  it("отменяет старую страницу и сбрасывает выбор до debounce поиска", async () => {
    readAssistantMock.mockResolvedValue(systemAssistant());
    readConversationsMock.mockResolvedValue({
      items: [conversation()],
      nextPageToken: "old",
    });
    const store = useAssistantStore();
    await store.load(context, "prj_sales");
    const pending = deferred<ListAssistantConversationsResponse>();
    readConversationsMock.mockReturnValueOnce(pending.promise);
    const more = store.loadMoreHistory();
    const oldSignal = readConversationsMock.mock.calls.at(
      -1,
    )?.[2] as AbortSignal;
    store.filterHistory("first", "ACTIVE");
    store.filterHistory("second", "ACTIVE");
    expect(oldSignal.aborted).toBe(true);
    expect(store.selectedRef).toBeUndefined();
    expect(store.nextPageToken).toBeUndefined();
    expect(store.conversations).toEqual([]);
    await vi.advanceTimersByTimeAsync(499);
    expect(readConversationsMock).toHaveBeenCalledTimes(2);
    readConversationsMock.mockResolvedValueOnce({ items: [] });
    await vi.advanceTimersByTimeAsync(1);
    expect(readConversationsMock).toHaveBeenLastCalledWith(
      "prj_sales",
      undefined,
      expect.any(AbortSignal),
      { query: "second", state: "ACTIVE" },
    );
    pending.resolve({ items: [conversation()] });
    await more;
    expect(store.conversations).toEqual([]);
  });
  it("перечитывает историю после архивации и не повторяет неопределённую команду", async () => {
    const source = conversation();
    const store = useAssistantStore();
    readAssistantMock.mockResolvedValue(systemAssistant());
    readConversationsMock.mockResolvedValue({ items: [source] });
    await store.load(context, "prj_sales");
    archiveConversationMock.mockRejectedValueOnce(new Error("Timeout"));
    await expect(store.archiveSelected()).rejects.toBeDefined();
    expect(store.selectedRef).toBe(source.ref);
    expect(archiveConversationMock).toHaveBeenCalledTimes(1);
    await store.archiveSelected();
    expect(archiveConversationMock).toHaveBeenCalledTimes(1);
    await store.load(context, "prj_sales");
    archiveConversationMock.mockResolvedValueOnce({
      ...source,
      state: "ARCHIVED",
      version: 3,
    });
    readConversationsMock.mockResolvedValueOnce({ items: [] });
    await store.archiveSelected();
    expect(store.selectedRef).toBeUndefined();
    expect(store.conversations).toEqual([]);
  });
  it("архивный диалог не принимает новые сообщения", async () => {
    const store = useAssistantStore();
    store.conversations = [{ ...conversation(), state: "ARCHIVED" }];
    store.selectedRef = "cnv_sales";
    await expect(store.send("text")).rejects.toThrow("read-only");
    expect(appendTurnMock).not.toHaveBeenCalled();
  });
  beforeEach(() => {
    vi.useFakeTimers();
    setActivePinia(createPinia());
    createConversationMock.mockReset();
    appendTurnMock.mockReset();
    archiveConversationMock.mockReset();
    applyPlanDraftMock.mockReset();
    readAssistantMock.mockReset();
    readConversationsMock.mockReset();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("добавляет cursor-страницу без потери выбранного диалога и понижения версии", async () => {
    readAssistantMock.mockResolvedValue(systemAssistant());
    readConversationsMock.mockResolvedValueOnce({
      items: [conversation()],
      nextPageToken: "next",
    });
    const store = useAssistantStore();
    await store.load(context, "prj_sales");
    store.selectedRef = "cnv_sales";
    readConversationsMock.mockResolvedValueOnce({
      items: [
        { ...conversation(), version: 1 },
        { ...conversation(), ref: "cnv_older" },
      ],
    });
    await store.loadMoreHistory();
    expect(readConversationsMock.mock.lastCall?.[1]).toBe("next");
    expect(store.conversations).toHaveLength(2);
    expect(store.selectedConversation?.version).toBe(2);
    expect(store.nextPageToken).toBeUndefined();
  });

  it("сохраняет выбранный диалог при realtime readback за первой страницей", async () => {
    readAssistantMock.mockResolvedValue(systemAssistant());
    const store = useAssistantStore();
    store.setContext(context, "prj_sales");
    store.conversations = [conversation()];
    store.selectedRef = "cnv_sales";
    readConversationsMock
      .mockResolvedValueOnce({
        items: [{ ...conversation(), ref: "cnv_new" }],
        nextPageToken: "next",
      })
      .mockResolvedValueOnce({
        items: [conversation()],
        nextPageToken: "remaining",
      });
    await store.load(context, "prj_sales");
    expect(store.selectedRef).toBe("cnv_sales");
    expect(store.conversations).toHaveLength(2);
    expect(store.nextPageToken).toBe("remaining");
  });

  it("отменяет in-flight страницу при смене project и не публикует поздний ответ", async () => {
    readAssistantMock.mockResolvedValue(systemAssistant());
    readConversationsMock.mockResolvedValueOnce({
      items: [conversation()],
      nextPageToken: "next",
    });
    const store = useAssistantStore();
    await store.load(context, "prj_sales");
    const pending = deferred<ListAssistantConversationsResponse>();
    readConversationsMock.mockReturnValueOnce(pending.promise);
    const operation = store.loadMoreHistory();
    const signal = readConversationsMock.mock.lastCall?.[2] as AbortSignal;
    store.setContext(context, "prj_other");
    expect(signal.aborted).toBe(true);
    expect(store.conversations).toEqual([]);
    pending.resolve({ items: [conversation()] });
    await operation;
    expect(store.conversations).toEqual([]);
    expect(store.loadingMore).toBe(false);
  });

  it("отклоняет чужой project и повторный cursor без добавления страницы", async () => {
    readAssistantMock.mockResolvedValue(systemAssistant());
    readConversationsMock.mockResolvedValueOnce({
      items: [conversation()],
      nextPageToken: "next",
    });
    const store = useAssistantStore();
    await store.load(context, "prj_sales");
    readConversationsMock.mockResolvedValueOnce({
      items: [{ ...conversation(), projectRef: "prj_other" }],
    });
    await store.loadMoreHistory();
    expect(store.historyProblem).toBeDefined();
    expect(store.conversations).toEqual([conversation()]);
    readConversationsMock.mockResolvedValueOnce({
      items: [{ ...conversation(), ref: "cnv_older" }],
      nextPageToken: "next",
    });
    await store.loadMoreHistory();
    expect(store.historyProblem).toBeDefined();
    expect(store.conversations).toEqual([conversation()]);
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
    expect(appendTurnMock).toHaveBeenCalledWith(created, "Создай сотрудника");
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

  it("не позволяет устаревшему load стереть созданный диалог", async () => {
    const assistantReadback = deferred<SystemAssistant>();
    const conversationsReadback =
      deferred<ListAssistantConversationsResponse>();
    const created = {
      ...conversation(),
      ref: "cnv_created_during_load",
      turns: [],
    };
    readAssistantMock.mockReturnValue(assistantReadback.promise);
    readConversationsMock.mockReturnValue(conversationsReadback.promise);
    createConversationMock.mockResolvedValue(created);
    const store = useAssistantStore();

    const loading = store.load(context, "prj_sales");
    await store.startConversation();
    assistantReadback.resolve(systemAssistant());
    conversationsReadback.resolve({ items: [] });
    await loading;

    expect(store.loading).toBe(false);
    expect(store.selectedRef).toBe(created.ref);
    expect(store.conversations).toEqual([created]);
  });

  it("возвращает нормализованную ошибку создания через problem state", async () => {
    createConversationMock.mockRejectedValue(new TypeError("network failed"));
    const store = useAssistantStore();
    store.setContext(context, "prj_sales");

    let failure: unknown;
    try {
      await store.startConversation();
    } catch (error: unknown) {
      failure = error;
    }

    expect(failure).toBeInstanceOf(AppProblem);
    expect(failure).toBe(store.problem);
    expect(store.problem).toMatchObject({
      code: "UNKNOWN",
      kind: "unavailable",
      retryable: true,
      status: 0,
    });
    expect(store.busy).toBe(false);

    readAssistantMock.mockResolvedValue(systemAssistant());
    readConversationsMock.mockResolvedValue({ items: [] });
    await store.load(context, "prj_sales");

    expect(store.problem).toBeUndefined();
    expect(store.loading).toBe(false);
  });

  it("очищает ошибку истории перед созданием диалога из realtime state", async () => {
    const created = { ...conversation(), turns: [] };
    readAssistantMock.mockResolvedValue(systemAssistant());
    readConversationsMock.mockRejectedValue(
      new TypeError("history unavailable"),
    );
    const store = useAssistantStore();

    await store.load(context, "prj_sales");
    store.applyRealtimeSnapshot(systemAssistant(), [], "prj_sales");

    expect(store.assistant?.runtimeState).toBe("READY");
    expect(store.assistant?.nextActions).toContain("CREATE_CONVERSATION");
    expect(store.problem).toMatchObject({
      code: "UNKNOWN",
      kind: "unavailable",
    });

    let problemDuringMutation: AppProblem | undefined;
    createConversationMock.mockImplementation(() => {
      problemDuringMutation = store.problem;
      return Promise.resolve(created);
    });

    await store.startConversation();

    expect(problemDuringMutation).toBeUndefined();
    expect(store.problem).toBeUndefined();
    expect(store.selectedRef).toBe(created.ref);
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
      initial,
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
