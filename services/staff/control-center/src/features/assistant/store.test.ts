import { createPinia, setActivePinia } from "pinia";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type {
  AssistantContextDescriptor,
  AssistantConversation,
  AssistantPlan,
  AssistantPlanReceipt,
  SystemAssistant,
  ListAssistantConversationsResponse,
  ProjectAssistantProfile,
  Agent,
  Run,
} from "@/shared/api/generated/openapi/types.gen";
import { AppProblem } from "@/shared/api/problem";
import { resetOwnerRequests } from "@/shared/api/owner-lifetime";

const createConversationMock = vi.hoisted(() => vi.fn());
const appendTurnMock = vi.hoisted(() => vi.fn());
const cancelAssistantTurnMock = vi.hoisted(() => vi.fn());
const archiveConversationMock = vi.hoisted(() => vi.fn());
const applyPlanDraftMock = vi.hoisted(() => vi.fn());
const readAssistantMock = vi.hoisted(() => vi.fn());
const readConversationsMock = vi.hoisted(() => vi.fn());
const readProjectAssistantMock = vi.hoisted(() => vi.fn());
const readProjectAssistantAgentMock = vi.hoisted(() => vi.fn());
const createProjectAssistantProfileMock = vi.hoisted(() => vi.fn());

vi.mock("@/features/assistant/api", () => ({
  readAssistant: readAssistantMock,
  readConversations: readConversationsMock,
  readProjectAssistant: readProjectAssistantMock,
  readProjectAssistantAgent: readProjectAssistantAgentMock,
  createProjectAssistantProfile: createProjectAssistantProfileMock,
  createConversation: createConversationMock,
  appendTurn: appendTurnMock,
  cancelAssistantTurn: cancelAssistantTurnMock,
  archiveConversation: archiveConversationMock,
  renameConversation: vi.fn(),
  savePlanDraft: vi.fn(),
  validatePlanDraft: vi.fn(),
  applyPlanDraft: applyPlanDraftMock,
  rejectPlanDraft: vi.fn(),
}));

import { useAssistantStore } from "@/features/assistant/store";
import { usePlatformStore } from "@/features/platform/store";
import { assistantRoleImageBuildTarget } from "@/features/assistant/model";
import {
  appliedFilePlanFixture,
  fileOperationFixture,
  requireFixture,
} from "./project-file-plan.fixtures";

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
    assistantScope: "SYSTEM",
    assistantRef: "ast_system_assistant",
    state: "ACTIVE",
    version: 2,
    title: "Настройка отдела продаж",
    titleSource: "AGENT_PROPOSED",
    titleRevision: 1,
    context,
    projectRef: "prj_sales",
    turns: [
      {
        source: { origin: "ORDINARY" as const },
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
    source: { origin: "ORDINARY" as const },
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
  const profile: ProjectAssistantProfile = {
    ref: "aprf_sales",
    projectRef: "prj_sales",
    agentRef: "agt_project_assistant",
    name: "Помощник продаж",
    state: "ACTIVE",
    version: 1,
    createdAt: "2026-10-03T00:00:00Z",
    updatedAt: "2026-10-03T00:00:00Z",
  };
  const projectAgent: Agent = {
    ref: profile.agentRef,
    projectRef: profile.projectRef,
    version: 1,
    name: profile.name,
    purpose: "Продажи",
    roleDescription: "Помощник",
    state: "DRAFT",
    enabled: true,
    system: false,
    runtimeRef: "runtime_sales",
    runtimeName: "Продажи",
    runtimeReady: false,
    capabilities: [],
    integrations: [],
    knowledgeArtifactRefs: [],
    nextActions: ["EDIT"],
    updatedAt: profile.updatedAt,
  };
  const projectConversation = (): AssistantConversation => ({
    ...conversation(),
    ref: "cnv_project_assistant",
    assistantScope: "PROJECT",
    assistantRef: profile.agentRef,
    assistantProfileRef: profile.ref,
    turns: [],
  });

  it("разрешает новый диалог при фоновой сверке старой страницы, не подтверждая выбранное сообщение", async () => {
    const selected = projectConversation();
    const older = { ...selected, ref: "cnv_older_project" };
    const pending = deferred<ListAssistantConversationsResponse>();
    readConversationsMock.mockReturnValue(pending.promise);
    const store = useAssistantStore();
    store.setContext(context, profile.projectRef);
    store.assistantScope = "PROJECT";
    store.projectAssistant = profile;
    store.projectAssistantAgent = projectAgent;
    store.assistant = systemAssistant();
    store.conversations = [selected, older];
    store.selectedRef = selected.ref;
    store.applyRealtimeSnapshot(
      systemAssistant(),
      [selected],
      profile.projectRef,
      "ws-partial",
    );
    expect(store.loading).toBe(true);
    expect(store.historyRefreshing).toBe(true);
    expect(store.conversationCreationReady).toBe(true);
    expect(store.selectedRef).toBe(selected.ref);
    store.projectAssistantAgent = undefined;
    expect(store.conversationCreationReady).toBe(false);
    store.projectAssistantAgent = { ...projectAgent, projectRef: "prj_other" };
    expect(store.conversationCreationReady).toBe(false);
    store.projectAssistantAgent = projectAgent;
    store.projectAssistant = { ...profile, state: "DISABLED" };
    expect(store.conversationCreationReady).toBe(false);
    store.projectAssistant = profile;
    store.busy = true;
    expect(store.conversationCreationReady).toBe(false);
    store.busy = false;
    pending.resolve({ items: [selected, older] });
    await vi.waitFor(() => expect(store.loading).toBe(false));
    expect(store.historyRefreshing).toBe(false);
    expect(store.conversationCreationReady).toBe(true);
  });

  it("не снимает initial loading barrier из-за частичного WS во время owner load", async () => {
    const selected = conversation();
    const older = { ...selected, ref: "cnv_older_initial" };
    const initial = deferred<ListAssistantConversationsResponse>();
    const refresh = deferred<ListAssistantConversationsResponse>();
    readConversationsMock
      .mockReturnValueOnce(initial.promise)
      .mockReturnValueOnce(refresh.promise);
    const store = useAssistantStore();
    store.setContext(context, "prj_sales");
    store.assistant = systemAssistant();
    store.conversations = [selected, older];
    store.selectedRef = selected.ref;
    const loading = store.load(context, "prj_sales");
    store.applyRealtimeSnapshot(
      systemAssistant(),
      [selected],
      "prj_sales",
      "ws-partial",
    );
    expect(store.conversationCreationReady).toBe(false);
    initial.resolve({ items: [selected, older] });
    await loading;
    expect(store.loading).toBe(true);
    refresh.resolve({ items: [selected, older] });
    await vi.waitFor(() => expect(store.loading).toBe(false));
  });

  it.each(["scope", "forbidden"] as const)(
    "закрывает background creation при %s и не принимает старую сверку",
    async (change) => {
      const selected = projectConversation();
      const older = { ...selected, ref: "cnv_older_revoked" };
      let reject: (reason: unknown) => void = () => {};
      const pending = deferred<ListAssistantConversationsResponse>();
      readConversationsMock.mockReturnValue(
        new Promise((resolve, fail) => {
          reject = fail;
          void pending.promise.then(resolve);
        }),
      );
      const store = useAssistantStore();
      store.setContext(context, profile.projectRef);
      store.assistantScope = "PROJECT";
      store.projectAssistant = profile;
      store.projectAssistantAgent = projectAgent;
      store.assistant = systemAssistant();
      store.conversations = [selected, older];
      store.selectedRef = selected.ref;
      store.applyRealtimeSnapshot(
        systemAssistant(),
        [selected],
        "prj_sales",
        "ws",
      );
      expect(store.conversationCreationReady).toBe(true);
      if (change === "scope") {
        store.setContext({ ...context, entityRef: "prj_other" }, "prj_other");
        store.assistantScope = "PROJECT";
        pending.resolve({ items: [selected, older] });
        await Promise.resolve();
        expect(store.historyRefreshing).toBe(false);
        expect(store.selectedConversation).toBeUndefined();
      } else {
        reject(
          new AppProblem({
            status: 403,
            code: "FORBIDDEN",
            retryable: false,
            kind: "forbidden",
          }),
        );
        await vi.waitFor(() => expect(store.loading).toBe(false));
        expect(store.selectedConversation).toBeUndefined();
      }
      expect(store.conversationCreationReady).toBe(false);
    },
  );

  it("после навигации не передаёт общий WS cursor в фильтрованную историю SYSTEM", async () => {
    const store = useAssistantStore();
    store.setContext(context, "prj_sales");
    store.setHistoryPageSize(22);
    store.applyRealtimeSnapshot(
      systemAssistant(),
      [conversation()],
      "prj_sales",
      "ws-cursor",
    );
    store.setContext(
      {
        ...context,
        route: "/projects/prj_sales/agents/agt_developer",
        entityKind: "AGENT",
        entityRef: "agt_developer",
      },
      "prj_sales",
    );
    store.setHistoryPageSize(17);
    readConversationsMock.mockResolvedValue({
      items: [conversation()],
      nextPageToken: "owner-17",
    });

    await store.loadMoreHistory(17);

    expect(readConversationsMock).toHaveBeenCalledExactlyOnceWith(
      "prj_sales",
      undefined,
      expect.any(AbortSignal),
      {
        query: "",
        state: "ACTIVE",
        assistantScope: "SYSTEM",
        assistantRef: systemAssistant().ref,
      },
      17,
    );
    expect(store.nextPageToken).toBe("owner-17");
    expect(store.selectedRef).toBe(conversation().ref);
    expect(store.historyProblem).toBeUndefined();
  });

  it("сохраняет фильтр owner cursor и адаптивный размер после viewport22→17", async () => {
    const store = useAssistantStore();
    store.assistant = systemAssistant();
    store.setHistoryPageSize(22);
    readConversationsMock
      .mockResolvedValueOnce({
        items: [conversation()],
        nextPageToken: "owner-22",
      })
      .mockResolvedValueOnce({
        items: [{ ...conversation(), ref: "cnv_more" }],
      });
    await store.load(context, "prj_sales");
    store.setHistoryPageSize(17);
    await store.loadMoreHistory(17);
    expect(readConversationsMock.mock.calls[1]?.[1]).toBe("owner-22");
    expect(readConversationsMock.mock.calls[1]?.[4]).toBe(17);
    expect(readConversationsMock.mock.calls[1]?.[3]).toEqual(
      readConversationsMock.mock.calls[0]?.[3],
    );
    expect(store.conversations).toHaveLength(2);
    await store.load(context, "prj_sales");
    expect(readConversationsMock.mock.calls[2]?.[4]).toBe(17);
  });

  it("не добавляет assistantRef к cursor первого параллельного SYSTEM чтения", async () => {
    readAssistantMock.mockResolvedValue(systemAssistant());
    readConversationsMock
      .mockResolvedValueOnce({
        items: [conversation()],
        nextPageToken: "owner-unpinned",
      })
      .mockResolvedValueOnce({ items: [] });
    const store = useAssistantStore();
    await store.load(context, "prj_sales");
    await store.loadMoreHistory();
    expect(readConversationsMock.mock.calls[1]?.[3]).toEqual(
      readConversationsMock.mock.calls[0]?.[3],
    );
  });

  it("сохраняет фильтр expanded owner cursor после rejoin и возвращает viewport17", async () => {
    const items = Array.from({ length: 42 }, (_, index) => ({
      ...conversation(),
      ref: `cnv_${String(index)}`,
    }));
    const store = useAssistantStore();
    store.setContext(context, "prj_sales");
    store.assistant = systemAssistant();
    store.setHistoryPageSize(17);
    store.conversations = items;
    store.selectedRef = "cnv_41";
    readConversationsMock
      .mockResolvedValueOnce({ items, nextPageToken: "expanded-42" })
      .mockResolvedValueOnce({ items: [] });
    store.applyRealtimeSnapshot(
      systemAssistant(),
      items.slice(0, 12),
      "prj_sales",
      "ws-next",
    );
    await vi.waitFor(() => expect(store.loading).toBe(false));
    await store.loadMoreHistory(17);
    expect(readConversationsMock.mock.calls[0]?.[4]).toBe(42);
    expect(readConversationsMock.mock.calls[1]?.[1]).toBe("expanded-42");
    expect(readConversationsMock.mock.calls[1]?.[4]).toBe(17);
  });

  it("fresh чтение вместо WS cursor объединяет повторный scroll и игнорирует late ACK после смены проекта", async () => {
    const first = deferred<ListAssistantConversationsResponse>();
    const store = useAssistantStore();
    store.setContext(context, "prj_sales");
    store.applyRealtimeSnapshot(
      systemAssistant(),
      [conversation()],
      "prj_sales",
      "ws-cursor",
    );
    readConversationsMock.mockReturnValueOnce(first.promise);
    const read = store.loadMoreHistory(17);
    await store.loadMoreHistory(17);
    expect(readConversationsMock).toHaveBeenCalledTimes(1);
    const signal = readConversationsMock.mock.calls[0]?.[2] as AbortSignal;
    store.setContext({ ...context, entityRef: "prj_other" }, "prj_other");
    expect(signal.aborted).toBe(true);
    first.resolve({ items: [conversation()], nextPageToken: "old-owner" });
    await read;
    expect(store.conversations).toEqual([]);
    expect(store.nextPageToken).toBeUndefined();
    expect(store.loading).toBe(false);
  });

  it.each([403, 404])(
    "fresh чтение вместо WS cursor сохраняет закрытый отказ %s без повторного GET",
    async (status) => {
      const store = useAssistantStore();
      store.setContext(context, "prj_sales");
      store.applyRealtimeSnapshot(
        systemAssistant(),
        [conversation()],
        "prj_sales",
        "ws-cursor",
      );
      readConversationsMock.mockRejectedValueOnce(
        new AppProblem({
          status,
          code: status === 403 ? "FORBIDDEN" : "NOT_FOUND",
          kind: status === 403 ? "forbidden" : "not-found",
          retryable: false,
        }),
      );
      await store.loadMoreHistory(17);
      await store.loadMoreHistory(17);
      expect(readConversationsMock).toHaveBeenCalledTimes(1);
      expect(readConversationsMock.mock.calls[0]?.[1]).toBeUndefined();
      expect(store.problem?.status).toBe(status);
      expect(store.conversations).toEqual([]);
      expect(store.selectedRef).toBeUndefined();
      expect(store.nextPageToken).toBeUndefined();
    },
  );

  it("смена SYSTEM pin начинает новую историю без старого owner cursor", async () => {
    const store = useAssistantStore();
    store.assistant = systemAssistant();
    readConversationsMock.mockResolvedValueOnce({
      items: [conversation()],
      nextPageToken: "old-owner",
    });
    await store.load(context, "prj_sales");
    store.assistant = { ...systemAssistant(), ref: "ast_new" };
    readConversationsMock.mockResolvedValueOnce({
      items: [{ ...conversation(), assistantRef: "ast_new" }],
    });
    await store.loadMoreHistory();
    expect(readConversationsMock.mock.calls[1]?.[1]).toBeUndefined();
    expect(readConversationsMock.mock.calls[1]?.[3]).toMatchObject({
      assistantScope: "SYSTEM",
      assistantRef: "ast_new",
    });
    expect(store.selectedConversation?.assistantRef).toBe("ast_new");
  });

  it("полный WS snapshot закрывает прежнюю cursor цепочку, новая partial страница требует свежий owner read", async () => {
    const store = useAssistantStore();
    store.assistant = systemAssistant();
    readConversationsMock.mockResolvedValueOnce({ items: [conversation()] });
    await store.load(context, "prj_sales");
    store.applyRealtimeSnapshot(
      systemAssistant(),
      [conversation()],
      "prj_sales",
    );
    expect(store.nextPageToken).toBeUndefined();
    store.applyRealtimeSnapshot(
      systemAssistant(),
      [conversation()],
      "prj_sales",
      "new-ws-page",
    );
    expect(store.nextPageToken).toBe("new-ws-page");
    readConversationsMock.mockResolvedValueOnce({
      items: [conversation()],
      nextPageToken: "fresh-owner",
    });
    await store.loadMoreHistory(17);
    expect(readConversationsMock.mock.calls[1]?.[1]).toBeUndefined();
    expect(store.nextPageToken).toBe("fresh-owner");
    expect(readConversationsMock).toHaveBeenCalledTimes(2);
  });

  it("поиск и ARCHIVED начинают отдельную owner цепочку и сохраняют её cursor", async () => {
    const store = useAssistantStore();
    store.assistant = systemAssistant();
    readConversationsMock.mockResolvedValueOnce({
      items: [conversation()],
      nextPageToken: "active-owner",
    });
    await store.load(context, "prj_sales");
    store.filterHistory("архив", "ARCHIVED");
    readConversationsMock.mockResolvedValueOnce({
      items: [{ ...conversation(), state: "ARCHIVED" }],
      nextPageToken: "archive-owner",
    });
    await vi.waitFor(() => expect(store.loading).toBe(false));
    readConversationsMock.mockResolvedValueOnce({ items: [] });
    await store.loadMoreHistory(17);
    expect(readConversationsMock.mock.calls[1]?.[1]).toBeUndefined();
    expect(readConversationsMock.mock.calls[2]?.[1]).toBe("archive-owner");
    expect(readConversationsMock.mock.calls[2]?.[3]).toEqual({
      query: "архив",
      state: "ARCHIVED",
      assistantScope: "SYSTEM",
      assistantRef: systemAssistant().ref,
    });
  });

  it("читает профиль Проекта отдельно и не смешивает историю двух помощников", async () => {
    readAssistantMock.mockResolvedValue(systemAssistant());
    readProjectAssistantMock.mockResolvedValue(profile);
    readProjectAssistantAgentMock.mockResolvedValue(projectAgent);
    readConversationsMock
      .mockResolvedValueOnce({ items: [conversation()] })
      .mockResolvedValueOnce({ items: [projectConversation()] });
    const store = useAssistantStore();
    await store.load(context, "prj_sales");
    await store.selectAssistantScope("PROJECT");
    expect(store.projectAssistant).toEqual(profile);
    expect(store.projectAssistantAgent).toEqual(projectAgent);
    expect(store.selectedConversation?.assistantRef).toBe(profile.agentRef);
    expect(readConversationsMock.mock.lastCall?.[3]).toMatchObject({
      assistantScope: "PROJECT",
      assistantRef: profile.agentRef,
    });
    store.applyRealtimeSnapshot(
      systemAssistant(),
      [conversation(), projectConversation()],
      "prj_sales",
    );
    expect(store.conversations.map((value) => value.assistantScope)).toEqual([
      "PROJECT",
    ]);
    store.setContext({ ...context, route: "/" }, undefined);
    expect(store.assistantScope).toBe("SYSTEM");
    expect(store.projectAssistant).toBeUndefined();
  });

  it("404 профиля не подменяет проектного помощника общесистемным", async () => {
    readProjectAssistantMock.mockRejectedValue(
      new AppProblem({
        status: 404,
        code: "NOT_FOUND",
        retryable: false,
        kind: "not-found",
      }),
    );
    const store = useAssistantStore();
    await store.load(context, "prj_sales", true, "PROJECT");
    expect(store.assistantScope).toBe("PROJECT");
    expect(store.projectAssistant).toBeUndefined();
    expect(store.conversations).toEqual([]);
    expect(store.problem).toBeUndefined();
    expect(readConversationsMock).not.toHaveBeenCalled();
  });

  it("перечитывает readiness только после realtime invalidation нужного Проекта", async () => {
    readAssistantMock.mockResolvedValue(systemAssistant());
    readProjectAssistantMock.mockResolvedValue(profile);
    readProjectAssistantAgentMock.mockResolvedValue(projectAgent);
    readConversationsMock.mockResolvedValue({ items: [projectConversation()] });
    const store = useAssistantStore();
    await store.load(context, "prj_sales", true, "PROJECT");
    const before = readProjectAssistantMock.mock.calls.length;
    await store.invalidateProjectAssistantFromRealtime("prj_other");
    expect(readProjectAssistantMock).toHaveBeenCalledTimes(before);
    readProjectAssistantAgentMock.mockResolvedValue({
      ...projectAgent,
      version: 2,
      runtimeReady: true,
      nextActions: ["EDIT", "LAUNCH"],
    });
    const refreshing =
      store.invalidateProjectAssistantFromRealtime("prj_sales");
    expect(store.conversationCreationReady).toBe(false);
    await refreshing;
    expect(store.conversationCreationReady).toBe(true);
    expect(readProjectAssistantMock).toHaveBeenCalledTimes(before + 1);
    expect(store.projectAssistantAgent?.runtimeReady).toBe(true);
    expect(store.selectedConversation?.assistantRef).toBe(profile.agentRef);
  });

  it("игнорирует поздний profile read после смены контекста", async () => {
    readAssistantMock.mockResolvedValue(systemAssistant());
    readProjectAssistantMock.mockResolvedValue(profile);
    readProjectAssistantAgentMock.mockResolvedValue(projectAgent);
    readConversationsMock.mockResolvedValue({ items: [] });
    const store = useAssistantStore();
    await store.load(context, "prj_sales", true, "PROJECT");
    const pending = deferred<ProjectAssistantProfile>();
    readProjectAssistantMock.mockReturnValueOnce(pending.promise);
    const refresh = store.invalidateProjectAssistantFromRealtime("prj_sales");
    store.setContext({ ...context, entityRef: "prj_other" }, "prj_other");
    pending.resolve(profile);
    await refresh;
    expect(store.projectAssistant).toBeUndefined();
    expect(store.projectAssistantAgent).toBeUndefined();
    expect(store.problem).toBeUndefined();
  });

  it("закрыто отклоняет чужую assistant history и сохраняет отдельный scope создания", async () => {
    readAssistantMock.mockResolvedValue(systemAssistant());
    readProjectAssistantMock.mockResolvedValue(profile);
    readProjectAssistantAgentMock.mockResolvedValue(projectAgent);
    readConversationsMock.mockResolvedValue({ items: [] });
    createConversationMock.mockResolvedValue(projectConversation());
    const store = useAssistantStore();
    await store.load(context, "prj_sales", true, "PROJECT");
    await store.startConversation();
    expect(createConversationMock).toHaveBeenCalledWith(
      context,
      "prj_sales",
      "PROJECT",
    );
    readConversationsMock.mockResolvedValue({ items: [conversation()] });
    await store.load(context, "prj_sales", true, "PROJECT");
    expect(store.problem).toBeDefined();
    expect(store.conversations).toEqual([]);
  });
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
      {
        query: "second",
        state: "ACTIVE",
        assistantScope: "SYSTEM",
        assistantRef: "ast_system_assistant",
      },
      40,
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
    resetOwnerRequests();
    vi.useFakeTimers();
    setActivePinia(createPinia());
    createConversationMock.mockReset();
    appendTurnMock.mockReset();
    cancelAssistantTurnMock.mockReset();
    archiveConversationMock.mockReset();
    applyPlanDraftMock.mockReset();
    readAssistantMock.mockReset();
    readConversationsMock.mockReset();
    readProjectAssistantMock.mockReset();
    readProjectAssistantAgentMock.mockReset();
    createProjectAssistantProfileMock.mockReset();
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it("после перезагрузки возвращает выбранный диалог со следующей страницы истории", async () => {
    const selected = conversation();
    const newer = {
      ...conversation(),
      ref: "cnv_newer",
      updatedAt: "2026-09-26T00:00:00Z",
    };
    vi.stubGlobal("window", {
      sessionStorage: {
        getItem: (key: string) =>
          key === "kodex.assistant.workspace.conversation.SYSTEM.prj_sales"
            ? selected.ref
            : null,
      },
    });
    readAssistantMock.mockResolvedValue(systemAssistant());
    readConversationsMock
      .mockResolvedValueOnce({ items: [newer], nextPageToken: "next" })
      .mockResolvedValueOnce({ items: [selected] });

    const store = useAssistantStore();
    await store.load(context, "prj_sales");

    expect(readConversationsMock).toHaveBeenCalledTimes(2);
    expect(store.selectedRef).toBe(selected.ref);
    expect(store.selectedConversation?.title).toBe(selected.title);
  });

  it.each([undefined, "prj_sales"])(
    "восстанавливает сохранённый SYSTEM диалог из scoped realtime snapshot %s",
    (scope) => {
      const selected = { ...conversation(), projectRef: scope };
      const newer = {
        ...selected,
        ref: "cnv_newer",
        updatedAt: "2026-09-26T00:00:00Z",
      };
      vi.stubGlobal("window", {
        sessionStorage: {
          getItem: (key: string) =>
            key ===
            `kodex.assistant.workspace.conversation.SYSTEM.${scope ?? "all"}`
              ? selected.ref
              : "cnv_foreign_scope",
        },
      });
      const store = useAssistantStore();
      store.setContext(context, scope);
      store.applyRealtimeSnapshot(systemAssistant(), [newer, selected], scope);

      expect(store.selectedRef).toBe(selected.ref);
      expect(readConversationsMock).not.toHaveBeenCalled();
      store.selectedRef = newer.ref;
      store.applyRealtimeSnapshot(systemAssistant(), [selected, newer], scope);
      expect(store.selectedRef).toBe(newer.ref);
    },
  );

  it("не принимает сохранённый ref с чужим project или assistant pin", () => {
    vi.stubGlobal("window", {
      sessionStorage: { getItem: () => "cnv_foreign" },
    });
    const store = useAssistantStore();
    store.setContext(context, "prj_sales");
    const foreign = { ...conversation(), ref: "cnv_foreign" };
    for (const invalid of [
      { ...foreign, projectRef: "prj_other" },
      { ...foreign, assistantRef: "agt_other" },
      { ...foreign, assistantProfileRef: "asstp_foreign" },
      { ...projectConversation(), ref: foreign.ref },
    ]) {
      store.selectedRef = undefined;
      store.applyRealtimeSnapshot(
        systemAssistant(),
        [invalid, conversation()],
        "prj_sales",
      );
      expect(store.selectedRef).toBe("cnv_sales");
    }
    expect(readConversationsMock).not.toHaveBeenCalled();
  });

  it("восстанавливает PROJECT выбор только с точным профилем и отдельным storage ключом", async () => {
    const selected = projectConversation();
    const newer = {
      ...selected,
      ref: "cnv_project_newer",
      updatedAt: "2026-09-26T00:00:00Z",
    };
    vi.stubGlobal("window", {
      sessionStorage: {
        getItem: (key: string) =>
          key === "kodex.assistant.workspace.conversation.PROJECT.prj_sales"
            ? selected.ref
            : "cnv_system_saved",
      },
    });
    readAssistantMock.mockResolvedValue(systemAssistant());
    readProjectAssistantMock.mockResolvedValue(profile);
    readProjectAssistantAgentMock.mockResolvedValue(projectAgent);
    readConversationsMock.mockResolvedValue({ items: [newer, selected] });
    const store = useAssistantStore();
    await store.load(context, "prj_sales", false, "PROJECT");
    const before = readConversationsMock.mock.calls.length;
    store.applyRealtimeSnapshot(
      systemAssistant(),
      [newer, selected, conversation()],
      "prj_sales",
    );
    expect(store.selectedRef).toBe(selected.ref);
    expect(store.conversations.map((value) => value.ref)).toEqual([
      newer.ref,
      selected.ref,
    ]);
    expect(readConversationsMock).toHaveBeenCalledTimes(before);
    store.selectedRef = newer.ref;
    store.applyRealtimeSnapshot(
      systemAssistant(),
      [selected, newer],
      "prj_sales",
    );
    expect(store.selectedRef).toBe(newer.ref);
  });

  it("после reload восстанавливает PROJECT режим до чтения истории и выбранный диалог", async () => {
    const selected = projectConversation();
    vi.stubGlobal("window", {
      sessionStorage: {
        getItem: (key: string) =>
          key === "kodex.assistant.workspace.scope.prj_sales"
            ? "PROJECT"
            : key === "kodex.assistant.workspace.conversation.PROJECT.prj_sales"
              ? selected.ref
              : null,
      },
    });
    readAssistantMock.mockResolvedValue(systemAssistant());
    readProjectAssistantMock.mockResolvedValue(profile);
    readProjectAssistantAgentMock.mockResolvedValue(projectAgent);
    readConversationsMock.mockResolvedValue({ items: [selected] });
    const store = useAssistantStore();
    store.setContext(context, "prj_sales");
    expect(store.assistantScope).toBe("PROJECT");
    await store.load(context, "prj_sales");
    expect(readProjectAssistantMock).toHaveBeenCalledWith(
      "prj_sales",
      expect.any(AbortSignal),
    );
    expect(readConversationsMock.mock.calls[0]?.[3]).toMatchObject({
      assistantScope: "PROJECT",
      assistantRef: profile.agentRef,
    });
    expect(store.selectedRef).toBe(selected.ref);
    expect(store.selectedConversation?.assistantProfileRef).toBe(profile.ref);
  });

  it("direct load и новый чат без saved ref используют сохранённый PROJECT режим", async () => {
    vi.stubGlobal("window", {
      sessionStorage: {
        getItem: (key: string) =>
          key === "kodex.assistant.workspace.scope.prj_sales"
            ? "PROJECT"
            : null,
      },
    });
    readAssistantMock.mockResolvedValue(systemAssistant());
    readProjectAssistantMock.mockResolvedValue(profile);
    readProjectAssistantAgentMock.mockResolvedValue(projectAgent);
    readConversationsMock.mockResolvedValue({ items: [] });
    createConversationMock.mockResolvedValue(projectConversation());
    const store = useAssistantStore();
    await store.load(context, "prj_sales");
    expect(store.assistantScope).toBe("PROJECT");
    await store.startConversation();
    expect(createConversationMock).toHaveBeenCalledWith(
      context,
      "prj_sales",
      "PROJECT",
    );
  });

  it("сохраняет явный SYSTEM выбор и не сбрасывает его повторным setContext", async () => {
    const values = new Map([
      ["kodex.assistant.workspace.scope.prj_sales", "PROJECT"],
    ]);
    vi.stubGlobal("window", {
      sessionStorage: {
        getItem: (key: string) => values.get(key) ?? null,
        setItem: (key: string, value: string) => values.set(key, value),
      },
    });
    readAssistantMock.mockResolvedValue(systemAssistant());
    readConversationsMock.mockResolvedValue({ items: [conversation()] });
    const store = useAssistantStore();
    store.setContext(context, "prj_sales");
    await store.selectAssistantScope("SYSTEM");
    expect(values.get("kodex.assistant.workspace.scope.prj_sales")).toBe(
      "SYSTEM",
    );
    store.setContext({ ...context }, "prj_sales");
    expect(store.assistantScope).toBe("SYSTEM");
  });

  it("не переносит режим и выбранный диалог между проектами или в общий контекст", () => {
    vi.stubGlobal("window", {
      sessionStorage: {
        getItem: (key: string) =>
          key === "kodex.assistant.workspace.scope.prj_sales"
            ? "PROJECT"
            : null,
      },
    });
    const store = useAssistantStore();
    store.setContext(context, "prj_sales");
    expect(store.assistantScope).toBe("PROJECT");
    store.conversations = [projectConversation()];
    store.selectedRef = projectConversation().ref;
    store.setContext({ ...context, entityRef: "prj_other" }, "prj_other");
    expect(store.assistantScope).toBe("SYSTEM");
    expect(store.selectedRef).toBeUndefined();
    expect(store.conversations).toEqual([]);
    store.setContext(context, "prj_sales");
    expect(store.assistantScope).toBe("PROJECT");
    store.setContext(context);
    expect(store.assistantScope).toBe("SYSTEM");
  });

  it("восстанавливает выбор после пустого initial snapshot и смены scope, не после ручного выбора", () => {
    vi.stubGlobal("window", {
      sessionStorage: {
        getItem: (key: string) =>
          key.endsWith(".prj_other") ? "cnv_other_selected" : "cnv_sales",
      },
    });
    const store = useAssistantStore();
    store.setContext(context, "prj_sales");
    store.applyRealtimeSnapshot(systemAssistant(), [], "prj_sales");
    expect(store.selectedRef).toBeUndefined();
    store.applyRealtimeSnapshot(
      systemAssistant(),
      [conversation()],
      "prj_sales",
    );
    expect(store.selectedRef).toBe("cnv_sales");
    store.setContext({ ...context, entityRef: "prj_other" }, "prj_other");
    const other = { ...conversation(), projectRef: "prj_other" };
    const selected = { ...other, ref: "cnv_other_selected" };
    store.applyRealtimeSnapshot(
      systemAssistant(),
      [other, selected],
      "prj_other",
    );
    expect(store.selectedRef).toBe(selected.ref);
  });

  it("не восстанавливает архивный сохранённый диалог в ACTIVE истории", () => {
    vi.stubGlobal("window", {
      sessionStorage: { getItem: () => "cnv_archived" },
    });
    const store = useAssistantStore();
    store.setContext(context, "prj_sales");
    const archived = {
      ...conversation(),
      ref: "cnv_archived",
      state: "ARCHIVED" as const,
    };
    store.applyRealtimeSnapshot(
      systemAssistant(),
      [conversation(), archived],
      "prj_sales",
    );
    expect(store.selectedRef).toBe("cnv_sales");
  });

  it("не ломает историю, если сохранённый диалог исчез из длинного списка", async () => {
    vi.stubGlobal("window", {
      sessionStorage: {
        getItem: () => "cnv_missing",
      },
    });
    readAssistantMock.mockResolvedValue(systemAssistant());
    let page = 0;
    readConversationsMock.mockImplementation(() => {
      page += 1;
      return Promise.resolve({
        items: page === 1 ? [conversation()] : [],
        nextPageToken: `next-${String(page)}`,
      });
    });

    const store = useAssistantStore();
    await store.load(context, "prj_sales");

    expect(readConversationsMock).toHaveBeenCalledTimes(30);
    expect(store.problem).toBeUndefined();
    expect(store.selectedRef).toBe("cnv_sales");
    expect(store.nextPageToken).toBe("next-30");
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

  it("сохраняет открытый project-диалог при переходе в общий scope", async () => {
    const staleGlobal = {
      ...conversation(),
      ref: "cnv_stale_global",
      projectRef: "prj_other",
      updatedAt: "2026-09-27T00:00:00Z",
    };
    vi.stubGlobal("window", {
      sessionStorage: {
        getItem: (key: string) =>
          key === "kodex.assistant.workspace.conversation.all"
            ? staleGlobal.ref
            : null,
      },
    });
    readAssistantMock.mockResolvedValue(systemAssistant());
    readConversationsMock
      .mockResolvedValueOnce({ items: [conversation()] })
      .mockResolvedValueOnce({ items: [staleGlobal, conversation()] });
    const store = useAssistantStore();
    await store.load(context, "prj_sales");

    await store.load(
      {
        route: "/decisions",
        entityKind: "ORGANIZATION",
        entityRef: "org_current",
        entityName: "Организация",
        entityVersion: 1,
        allowedOperations: [],
      },
      undefined,
    );

    expect(store.selectedRef).toBe("cnv_sales");
    expect(store.selectedConversation?.projectRef).toBe("prj_sales");
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

    expect(createConversationMock).toHaveBeenCalledWith(
      context,
      "prj_sales",
      "SYSTEM",
    );
    expect(appendTurnMock).toHaveBeenCalledWith(
      created,
      "Создай сотрудника",
      context,
      undefined,
      "QUEUE",
    );
    expect(store.selectedConversation?.turns).toHaveLength(1);
  });

  it.each(["ERROR", "PURGED"] as const)(
    "не отправляет и не ставит в очередь сообщение при exact session %s",
    async (storageState) => {
      const store = useAssistantStore();
      store.assistant = systemAssistant();
      store.setContext(context, "prj_sales");
      const selected = {
        ...conversation(),
        turns: [
          { ...userTurn("FAILED"), runRef: "run_storage", runVersion: 2 },
        ],
      };
      store.conversations = [selected];
      store.selectedRef = selected.ref;
      const platform = usePlatformStore();
      platform.bootstrap = {
        organizationRef: "org_storage",
      } as NonNullable<typeof platform.bootstrap>;
      platform.runs.run_storage = {
        ref: "run_storage",
        version: 2,
        projectRef: selected.projectRef,
        sessionRef: "ses_storage",
        state: "FAILED",
        source: "SYSTEM_ASSISTANT",
        target: { type: "SYSTEM_ASSISTANT", ref: selected.assistantRef },
        assistantPin: {
          scope: selected.assistantScope,
          organizationRef: "org_storage",
          conversationRef: selected.ref,
          assistantRef: selected.assistantRef,
          projectRef: selected.projectRef,
        },
        sessionReadiness: {
          sessionRef: "ses_storage",
          storageState,
          reason: "STORAGE_NOT_LIVE",
        },
      } as Run;
      for (const mode of ["QUEUE", "INTERRUPT_ACTIVE"] as const) {
        await expect(
          store.send("Сохранённый ввод", undefined, mode),
        ).rejects.toThrow("session storage is unavailable");
      }
      expect(appendTurnMock).not.toHaveBeenCalled();
      expect(createConversationMock).not.toHaveBeenCalled();
      expect(store.selectedConversation).toEqual(selected);
      expect(store.busy).toBe(false);
      expect(store.sessionStorageBlocker).toBe(storageState);
      const currentRun = platform.runs.run_storage;
      currentRun.state = "RUNNING";
      const currentTurn = selected.turns[0];
      if (!currentTurn) throw new Error("Synthetic USER turn is missing");
      currentTurn.state = "RUNNING";
      cancelAssistantTurnMock.mockResolvedValue("run_storage");
      await store.stopActiveTurn();
      expect(cancelAssistantTurnMock).toHaveBeenCalledOnce();
      expect(store.selectedConversation?.turns[0]?.state).toBe("CANCELLED");
      const fresh = { ...selected, ref: "cnv_storage_new", turns: [] };
      createConversationMock.mockResolvedValue(fresh);
      await store.startConversation();
      expect(store.selectedConversation?.ref).toBe(fresh.ref);
      expect(store.sessionStorageBlocker).toBeUndefined();
      expect(appendTurnMock).not.toHaveBeenCalled();
      expect(
        store.conversations.find((item) => item.ref === selected.ref)?.turns[0]
          ?.content,
      ).toBe("Создай сотрудника");
    },
  );

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

    expect(createConversationMock).toHaveBeenCalledWith(
      context,
      "prj_sales",
      "SYSTEM",
    );
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
      context,
      "aset_contracts",
      "QUEUE",
    );
  });

  it("передаёт выбранный режим доставки и оптимистично останавливает точный ход", async () => {
    const initial = {
      ...conversation(),
      version: 4,
      turns: [
        {
          ...userTurn("RUNNING"),
          runRef: "run_active",
          runVersion: 2,
        },
      ],
    };
    const accepted = {
      ...initial,
      version: 5,
      turns: [
        ...initial.turns,
        {
          ...userTurn("QUEUED"),
          ref: "trn_immediate",
          sequence: 3,
          content: "Выполни это сейчас",
          runRef: "run_immediate",
        },
      ],
    };
    appendTurnMock.mockResolvedValue(accepted);
    cancelAssistantTurnMock.mockResolvedValue("run_immediate");
    const store = useAssistantStore();
    store.setContext(context, "prj_sales");
    store.conversations = [initial];
    store.selectedRef = initial.ref;

    await store.send("Выполни это сейчас", undefined, "INTERRUPT_ACTIVE");
    expect(appendTurnMock).toHaveBeenCalledWith(
      initial,
      "Выполни это сейчас",
      context,
      undefined,
      "INTERRUPT_ACTIVE",
    );

    await store.stopActiveTurn();
    expect(cancelAssistantTurnMock).toHaveBeenCalledWith(accepted);
    expect(
      store.selectedConversation?.turns.find(
        (turn) => turn.runRef === "run_immediate",
      )?.state,
    ).toBe("CANCELLED");
    expect(
      store.selectedConversation?.turns.find(
        (turn) => turn.runRef === "run_active",
      )?.state,
    ).toBe("RUNNING");
  });

  it("Stop после позднего ответа отменяет только серверный run, сохраняя очередь", async () => {
    const current: AssistantConversation = {
      ...conversation(),
      turns: [
        {
          ...userTurn("COMPLETED"),
          ref: "trn_first",
          sequence: 1,
          runRef: "run_first",
        },
        {
          ...userTurn("RUNNING"),
          ref: "trn_q1",
          sequence: 2,
          runRef: "run_q1",
        },
        { ...userTurn("QUEUED"), ref: "trn_q2", sequence: 3, runRef: "run_q2" },
        {
          ...userTurn("COMPLETED"),
          ref: "trn_reply",
          sequence: 4,
          role: "ASSISTANT",
          runRef: "run_first",
        },
      ],
    };
    cancelAssistantTurnMock.mockResolvedValue("run_q1");
    const store = useAssistantStore();
    store.setContext(context, "prj_sales");
    store.conversations = [current];
    store.selectedRef = current.ref;
    await store.stopActiveTurn();
    expect(cancelAssistantTurnMock).toHaveBeenCalledExactlyOnceWith(current);
    expect(store.selectedConversation?.turns.map((turn) => turn.state)).toEqual(
      ["COMPLETED", "CANCELLED", "QUEUED", "COMPLETED"],
    );
  });

  it.each(["CLOSED", "ARCHIVED"] as const)(
    "Stop не отправляет mutation для %s",
    async (state) => {
      const current: AssistantConversation = {
        ...conversation(),
        state,
        turns: [{ ...userTurn("RUNNING"), runRef: "run_old" }],
      };
      const store = useAssistantStore();
      store.setContext(context, "prj_sales");
      store.conversations = [current];
      store.selectedRef = current.ref;
      await store.stopActiveTurn();
      expect(cancelAssistantTurnMock).not.toHaveBeenCalled();
    },
  );

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
          source: { origin: "ORDINARY" as const },
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
    ).toBe(true);
    expect(vi.getTimerCount()).toBe(0);
    expect(readConversationsMock).not.toHaveBeenCalled();

    store.applyRealtimeSnapshot(systemAssistant(), [completed], "prj_sales");

    expect(store.selectedConversation?.version).toBe(5);
    expect(store.selectedConversation?.turns.at(-1)?.content).toBe(
      "План готов",
    );
  });

  it.each(["SYSTEM", "PROJECT"] as const)(
    "%s сохраняет новый выбранный диалог, пока realtime обновляет прежний ход",
    async (scope) => {
      const base = scope === "PROJECT" ? projectConversation() : conversation();
      const running = { ...base, turns: [userTurn("RUNNING")] };
      const created = {
        ...base,
        ref: "cnv_created",
        title: "Новый диалог",
        turns: [],
      };
      const store = useAssistantStore();
      store.setContext(context, "prj_sales");
      store.assistant = systemAssistant();
      store.assistantScope = scope;
      if (scope === "PROJECT") store.projectAssistant = profile;
      store.conversations = [running];
      store.selectedRef = running.ref;
      createConversationMock.mockResolvedValue(created);

      await store.startConversation();
      expect(store.selectedRef).toBe(created.ref);
      const updated = { ...running, version: running.version + 1 };
      store.applyRealtimeSnapshot(systemAssistant(), [updated], "prj_sales");

      expect(store.selectedRef).toBe(created.ref);
      expect(store.selectedConversation?.turns).toEqual([]);
      expect(
        store.conversations.find((item) => item.ref === running.ref)?.version,
      ).toBe(updated.version);
      store.selectedRef = running.ref;
      store.applyRealtimeSnapshot(systemAssistant(), [updated], "prj_sales");
      expect(store.selectedRef).toBe(running.ref);
      expect(store.conversations.some((item) => item.ref === created.ref)).toBe(
        true,
      );

      store.selectedRef = created.ref;
      store.applyRealtimeSnapshot(
        systemAssistant(),
        [updated, { ...created, version: created.version - 1 }],
        "prj_sales",
      );
      store.applyRealtimeSnapshot(systemAssistant(), [updated], "prj_sales");
      expect(store.selectedRef).toBe(created.ref);
      store.applyRealtimeSnapshot(
        systemAssistant(),
        [updated, created],
        "prj_sales",
      );
      store.applyRealtimeSnapshot(systemAssistant(), [updated], "prj_sales");
      expect(store.selectedRef).toBe(running.ref);
      expect(store.conversations.some((item) => item.ref === created.ref)).toBe(
        false,
      );
    },
  );

  it("сохраняет диалог, автоматически созданный при отправке, до realtime readback", async () => {
    const created = { ...conversation(), ref: "cnv_created", turns: [] };
    const appended = {
      ...created,
      version: created.version + 1,
      turns: [userTurn("RUNNING")],
    };
    const store = useAssistantStore();
    store.setContext(context, "prj_sales");
    store.assistant = systemAssistant();
    store.conversations = [conversation()];
    store.selectedRef = undefined;
    createConversationMock.mockResolvedValue(created);
    appendTurnMock.mockResolvedValue(appended);

    await store.send("Новый запрос");
    store.applyRealtimeSnapshot(
      systemAssistant(),
      [conversation()],
      "prj_sales",
    );

    expect(store.selectedRef).toBe(created.ref);
    expect(store.selectedConversation?.version).toBe(appended.version);
    expect(store.selectedConversation?.turns).toEqual(appended.turns);
  });

  it("не переносит ещё не доставленный в realtime новый диалог в другой проект", async () => {
    const created = { ...conversation(), ref: "cnv_created", turns: [] };
    const store = useAssistantStore();
    store.setContext(context, "prj_sales");
    store.assistant = systemAssistant();
    createConversationMock.mockResolvedValue(created);
    await store.startConversation();

    store.setContext({ ...context, entityRef: "prj_other" }, "prj_other");
    const other = {
      ...conversation(),
      ref: "cnv_other",
      projectRef: "prj_other",
    };
    store.applyRealtimeSnapshot(systemAssistant(), [other], "prj_other");
    expect(store.selectedRef).toBe(other.ref);
    expect(store.conversations.map((item) => item.ref)).toEqual([other.ref]);
    store.setContext(context, "prj_sales");
    store.applyRealtimeSnapshot(systemAssistant(), [], "prj_sales");
    expect(store.conversations).toEqual([]);
    expect(store.selectedRef).toBeUndefined();
  });

  it("не сбрасывает вручную выбранный диалог при realtime из другого контекста проекта", () => {
    const selected = conversation();
    const environmentContext: AssistantContextDescriptor = {
      ...context,
      route: "/projects/prj_sales/environments/renv_test",
      entityKind: "ENVIRONMENT",
      entityRef: "renv_test",
      entityName: "Тестовая среда",
    };
    const store = useAssistantStore();
    store.setContext(environmentContext, "prj_sales");
    store.conversations = [selected];
    store.selectedRef = selected.ref;

    store.applyRealtimeSnapshot(systemAssistant(), [selected], "prj_sales");

    expect(store.selectedRef).toBe(selected.ref);
    expect(store.selectedConversation?.context.route).toBe(context.route);
  });

  it("сверяет выбранный диалог вне частичного snapshot и сохраняет owner cursor", async () => {
    const older = { ...conversation(), ref: "cnv_older" };
    const readback = deferred<{
      items: AssistantConversation[];
      nextPageToken: string;
    }>();
    readConversationsMock.mockReturnValue(readback.promise);
    const store = useAssistantStore();
    store.setContext(context, "prj_sales");
    store.assistant = systemAssistant();
    store.conversations = [conversation(), older];
    store.selectedRef = older.ref;
    store.nextPageToken = "owner-next";

    store.applyRealtimeSnapshot(
      systemAssistant(),
      [conversation()],
      "prj_sales",
      "ws-next",
    );

    expect(store.selectedRef).toBe(older.ref);
    expect(store.selectedConversation?.ref).toBe(older.ref);
    expect(store.loading).toBe(true);
    expect(store.nextPageToken).toBe("owner-next");
    expect(readConversationsMock).toHaveBeenCalledTimes(1);
    readback.resolve({
      items: [conversation(), older],
      nextPageToken: "fresh-owner-next",
    });
    await vi.waitFor(() => expect(store.loading).toBe(false));
    expect(store.selectedRef).toBe(older.ref);
    expect(store.nextPageToken).toBe("fresh-owner-next");
  });

  it("сжатие WS страницы 42→12 требует только одного owner read", async () => {
    const items = Array.from({ length: 42 }, (_, index) => ({
      ...conversation(),
      ref: `cnv_${String(index)}`,
    }));
    readConversationsMock.mockResolvedValue({ items });
    const store = useAssistantStore();
    store.setContext(context, "prj_sales");
    store.assistant = systemAssistant();
    store.conversations = items;
    store.selectedRef = "cnv_41";
    store.applyRealtimeSnapshot(
      systemAssistant(),
      items.slice(0, 12),
      "prj_sales",
      "ws-next",
    );
    await vi.waitFor(() => expect(store.loading).toBe(false));
    expect(readConversationsMock).toHaveBeenCalledTimes(1);
    expect(readConversationsMock.mock.calls[0]?.[4]).toBe(42);
    expect(store.selectedRef).toBe("cnv_41");
    expect(store.conversations).toHaveLength(42);
  });

  it("объединяет partial invalidations без перекрывающихся owner reads", async () => {
    const older = { ...conversation(), ref: "cnv_older" };
    const first = deferred<{ items: AssistantConversation[] }>();
    const second = deferred<{ items: AssistantConversation[] }>();
    readConversationsMock
      .mockReturnValueOnce(first.promise)
      .mockReturnValueOnce(second.promise);
    const store = useAssistantStore();
    store.setContext(context, "prj_sales");
    store.assistant = systemAssistant();
    store.conversations = [conversation(), older];
    store.selectedRef = older.ref;
    store.applyRealtimeSnapshot(
      systemAssistant(),
      [conversation()],
      "prj_sales",
      "ws-1",
    );
    store.applyRealtimeSnapshot(
      systemAssistant(),
      [{ ...conversation(), version: 9 }],
      "prj_sales",
      "ws-2",
    );
    expect(readConversationsMock).toHaveBeenCalledTimes(1);
    first.resolve({ items: [conversation(), older] });
    await vi.waitFor(() =>
      expect(readConversationsMock).toHaveBeenCalledTimes(2),
    );
    expect(store.selectedRef).toBe(older.ref);
    expect(store.loading).toBe(true);
    second.resolve({ items: [conversation(), older] });
    await vi.waitFor(() => expect(store.loading).toBe(false));
    expect(store.selectedRef).toBe(older.ref);
    expect(
      store.conversations.find((item) => item.ref === "cnv_sales")?.version,
    ).toBe(9);
  });

  it("burst partial snapshots не откладывает terminal owner read до затишья", async () => {
    const queued = {
      ...conversation(),
      ref: "cnv_older",
      turns: [userTurn("QUEUED")],
    };
    const failed = {
      ...queued,
      version: 25,
      turns: [userTurn("FAILED")],
    };
    const reads = Array.from({ length: 4 }, () =>
      deferred<{ items: AssistantConversation[] }>(),
    );
    for (const read of reads)
      readConversationsMock.mockReturnValueOnce(read.promise);
    const store = useAssistantStore();
    store.setContext(context, "prj_sales");
    store.assistant = systemAssistant();
    store.conversations = [conversation(), queued];
    store.selectedRef = queued.ref;
    store.applyRealtimeSnapshot(
      systemAssistant(),
      [conversation()],
      "prj_sales",
      "ws-start",
    );

    for (let index = 0; index < 3; index += 1) {
      for (let wake = 0; wake < 8; wake += 1)
        store.applyRealtimeSnapshot(
          systemAssistant(),
          [conversation()],
          "prj_sales",
          `ws-${String(index)}-${String(wake)}`,
        );
      expect(readConversationsMock).toHaveBeenCalledTimes(index + 1);
      reads[index]?.resolve({ items: [conversation(), failed] });
      await vi.waitFor(() =>
        expect(readConversationsMock).toHaveBeenCalledTimes(index + 2),
      );
      expect(store.selectedConversation?.version).toBe(25);
      expect(store.selectedConversation?.turns[0]?.state).toBe("FAILED");
      expect(store.loading).toBe(true);
    }
    reads[3]?.resolve({ items: [conversation(), failed] });
    await vi.waitFor(() => expect(store.loading).toBe(false));
    expect(readConversationsMock).toHaveBeenCalledTimes(4);
  });

  it("owner EOF удаляет selected detail даже при coalesced follow-up", async () => {
    const first = deferred<{ items: AssistantConversation[] }>();
    const second = deferred<{ items: AssistantConversation[] }>();
    readConversationsMock
      .mockReturnValueOnce(first.promise)
      .mockReturnValueOnce(second.promise);
    const store = useAssistantStore();
    store.setContext(context, "prj_sales");
    store.assistant = systemAssistant();
    store.conversations = [
      conversation(),
      { ...conversation(), ref: "cnv_gone" },
    ];
    store.selectedRef = "cnv_gone";
    for (let wake = 0; wake < 8; wake += 1)
      store.applyRealtimeSnapshot(
        systemAssistant(),
        [conversation()],
        "prj_sales",
        "ws-next",
      );
    first.resolve({ items: [conversation()] });
    await vi.waitFor(() =>
      expect(readConversationsMock).toHaveBeenCalledTimes(2),
    );
    expect(store.selectedConversation?.ref).not.toBe("cnv_gone");
    expect(store.conversations.some((item) => item.ref === "cnv_gone")).toBe(
      false,
    );
    second.resolve({ items: [conversation()] });
    await vi.waitFor(() => expect(store.loading).toBe(false));
  });

  it("coalesced owner read не заменяет более новую conversation version", async () => {
    const first = deferred<{ items: AssistantConversation[] }>();
    const second = deferred<{ items: AssistantConversation[] }>();
    readConversationsMock
      .mockReturnValueOnce(first.promise)
      .mockReturnValueOnce(second.promise);
    const store = useAssistantStore();
    store.setContext(context, "prj_sales");
    store.assistant = systemAssistant();
    const latest = {
      ...conversation(),
      version: 30,
      turns: [userTurn("FAILED")],
    };
    const older = { ...conversation(), ref: "cnv_older" };
    store.conversations = [conversation(), older];
    store.selectedRef = latest.ref;
    store.applyRealtimeSnapshot(
      systemAssistant(),
      [conversation()],
      "prj_sales",
      "ws-1",
    );
    store.applyRealtimeSnapshot(
      systemAssistant(),
      [latest],
      "prj_sales",
      "ws-2",
    );
    first.resolve({ items: [conversation(), older] });
    await vi.waitFor(() =>
      expect(readConversationsMock).toHaveBeenCalledTimes(2),
    );
    expect(store.selectedConversation?.version).toBe(30);
    expect(store.selectedConversation?.turns[0]?.state).toBe("FAILED");
    second.resolve({ items: [conversation(), older] });
    await vi.waitFor(() => expect(store.loading).toBe(false));
    expect(store.selectedConversation?.version).toBe(30);
    expect(store.selectedConversation?.turns[0]?.state).toBe("FAILED");
  });

  it.each(["gone", "forbidden"])(
    "сверка %s убирает отсутствующий selected detail",
    async (result) => {
      const store = useAssistantStore();
      store.setContext(context, "prj_sales");
      store.assistant = systemAssistant();
      store.conversations = [
        conversation(),
        { ...conversation(), ref: "cnv_gone" },
      ];
      store.selectedRef = "cnv_gone";
      if (result === "gone")
        readConversationsMock.mockResolvedValue({ items: [conversation()] });
      else
        readConversationsMock.mockRejectedValue(
          new AppProblem({
            status: 403,
            code: "FORBIDDEN",
            retryable: false,
            kind: "forbidden",
          }),
        );
      store.applyRealtimeSnapshot(
        systemAssistant(),
        [conversation()],
        "prj_sales",
        "ws-next",
      );
      await vi.waitFor(() => expect(store.loading).toBe(false));
      expect(store.selectedConversation?.ref).not.toBe("cnv_gone");
      expect(store.conversations.some((item) => item.ref === "cnv_gone")).toBe(
        false,
      );
      if (result === "forbidden") expect(store.conversations).toEqual([]);
    },
  );

  it.each(["scope", "owner", "complete", "unavailable"])(
    "поздний readback не проходит границу %s",
    async (boundary) => {
      const response = deferred<{ items: AssistantConversation[] }>();
      readConversationsMock.mockReturnValue(response.promise);
      const store = useAssistantStore();
      store.setContext(context, "prj_sales");
      store.assistant = systemAssistant();
      const older = { ...conversation(), ref: "cnv_older" };
      store.conversations = [conversation(), older];
      store.selectedRef = older.ref;
      store.applyRealtimeSnapshot(
        systemAssistant(),
        [conversation()],
        "prj_sales",
        "ws-next",
      );
      store.applyRealtimeSnapshot(
        systemAssistant(),
        [conversation()],
        "prj_sales",
        "ws-coalesced",
      );
      if (boundary === "scope")
        store.setContext({ ...context, entityRef: "prj_other" }, "prj_other");
      else if (boundary === "owner") resetOwnerRequests();
      else if (boundary === "complete")
        store.applyRealtimeSnapshot(systemAssistant(), [], "prj_sales");
      else store.clearRealtimeState();
      response.resolve({ items: [conversation(), older] });
      await vi.waitFor(() => expect(store.loading).toBe(false));
      expect(readConversationsMock).toHaveBeenCalledTimes(1);
      expect(store.conversations).toEqual([]);
      expect(store.selectedConversation).toBeUndefined();
    },
  );

  it("сверяет PROJECT историю отдельно от полной SYSTEM страницы", async () => {
    readConversationsMock.mockResolvedValue({ items: [projectConversation()] });
    const store = useAssistantStore();
    store.setContext(context, "prj_sales");
    store.assistant = systemAssistant();
    store.assistantScope = "PROJECT";
    store.projectAssistant = profile;
    store.conversations = [projectConversation()];
    store.selectedRef = "cnv_project_assistant";
    store.applyRealtimeSnapshot(
      systemAssistant(),
      [conversation()],
      "prj_sales",
    );
    await vi.waitFor(() => expect(store.loading).toBe(false));
    expect(store.selectedConversation?.assistantProfileRef).toBe(profile.ref);
    expect(readConversationsMock).toHaveBeenCalledWith(
      "prj_sales",
      undefined,
      expect.any(AbortSignal),
      expect.objectContaining({
        assistantScope: "PROJECT",
        assistantRef: profile.agentRef,
      }),
      40,
    );
    expect(store.conversations).toEqual([projectConversation()]);
  });

  it("смена profile pin закрывает pending readonly detail", async () => {
    const response = deferred<{ items: AssistantConversation[] }>();
    readConversationsMock.mockReturnValue(response.promise);
    const store = useAssistantStore();
    store.setContext(context, "prj_sales");
    store.assistant = systemAssistant();
    store.assistantScope = "PROJECT";
    store.projectAssistant = profile;
    store.conversations = [projectConversation()];
    store.selectedRef = "cnv_project_assistant";
    store.applyRealtimeSnapshot(
      systemAssistant(),
      [conversation()],
      "prj_sales",
      "ws-next",
    );
    store.projectAssistant = {
      ...profile,
      ref: "aprf_other",
      agentRef: "agt_other",
    };
    response.resolve({ items: [projectConversation()] });
    await vi.waitFor(() => expect(store.loading).toBe(false));
    expect(store.conversations).toEqual([]);
    expect(store.selectedConversation).toBeUndefined();
    expect(store.projectAssistant.ref).toBe("aprf_other");
  });

  it("не принимает чужой project при owner read", async () => {
    readConversationsMock.mockResolvedValue({
      items: [{ ...conversation(), projectRef: "prj_other" }],
    });
    const store = useAssistantStore();
    store.setContext(context, "prj_sales");
    store.assistant = systemAssistant();
    store.conversations = [
      conversation(),
      { ...conversation(), ref: "cnv_older" },
    ];
    store.applyRealtimeSnapshot(
      systemAssistant(),
      [conversation()],
      "prj_sales",
      "ws-next",
    );
    await vi.waitFor(() => expect(store.loading).toBe(false));
    expect(store.conversations).toEqual([]);
    expect(store.problem).toBeDefined();
  });

  it("сохраняет загруженные owner страницы и продолжение вместо WS cursor", async () => {
    const older = { ...conversation(), ref: "cnv_older" };
    readAssistantMock.mockResolvedValue(systemAssistant());
    readConversationsMock
      .mockResolvedValueOnce({
        items: [conversation()],
        nextPageToken: "owner-2",
      })
      .mockResolvedValueOnce({ items: [older], nextPageToken: "owner-3" })
      .mockResolvedValueOnce({
        items: [conversation()],
        nextPageToken: "new-owner-2",
      })
      .mockResolvedValueOnce({ items: [older], nextPageToken: "new-owner-3" });
    const store = useAssistantStore();
    await store.load(context, "prj_sales");
    await store.loadMoreHistory();
    store.selectedRef = older.ref;
    store.applyRealtimeSnapshot(
      systemAssistant(),
      [conversation()],
      "prj_sales",
      "ws-next",
    );
    expect(store.nextPageToken).toBe("owner-3");
    await vi.waitFor(() => expect(store.loading).toBe(false));
    expect(store.conversations.map((item) => item.ref)).toEqual([
      "cnv_sales",
      "cnv_older",
    ]);
    expect(store.nextPageToken).toBe("new-owner-3");
  });

  it("owner read ограничен 10 страницами и оставляет реальную дозагрузку", async () => {
    let page = 0;
    readConversationsMock.mockImplementation(() => {
      page += 1;
      return Promise.resolve({
        items: [{ ...conversation(), ref: `cnv_page_${String(page)}` }],
        nextPageToken: `owner-${String(page)}`,
      });
    });
    const store = useAssistantStore();
    store.setContext(context, "prj_sales");
    store.assistant = systemAssistant();
    store.conversations = [
      conversation(),
      { ...conversation(), ref: "cnv_after_limit" },
    ];
    store.selectedRef = "cnv_after_limit";
    store.applyRealtimeSnapshot(
      systemAssistant(),
      [conversation()],
      "prj_sales",
      "ws-next",
    );
    await vi.waitFor(() => expect(store.loading).toBe(false));
    expect(readConversationsMock).toHaveBeenCalledTimes(10);
    expect(store.selectedConversation?.ref).not.toBe("cnv_after_limit");
    expect(store.nextPageToken).toBe("owner-10");
    await store.loadMoreHistory();
    expect(readConversationsMock.mock.calls.at(-1)?.[1]).toBe("owner-10");
  });

  it("после загрузки показывает последний диалог проекта, если контекст экрана не совпал", async () => {
    const source = conversation();
    const environmentContext: AssistantContextDescriptor = {
      ...context,
      route: "/projects/prj_sales/environments/renv_test",
      entityKind: "ENVIRONMENT",
      entityRef: "renv_test",
      entityName: "Тестовая среда",
    };
    readAssistantMock.mockResolvedValue(systemAssistant());
    readConversationsMock.mockResolvedValue({ items: [source] });
    const store = useAssistantStore();

    await store.load(environmentContext, "prj_sales");

    expect(store.selectedRef).toBe(source.ref);
  });

  it("сохраняет conflict receipt и авторитетный STALE plan без частичного успеха", async () => {
    const stale = plan("STALE");
    applyPlanDraftMock.mockResolvedValue({
      conversation: { ref: "cnv_sales" },
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
    expect(store.sortedConversations[0]?.title).toBe("Настройка отдела продаж");
  });

  it("не подменяет полный диалог ответом применения с одним ref", async () => {
    const applied = { ...plan(), state: "APPLIED" as const, applied: true };
    applyPlanDraftMock.mockResolvedValue({
      conversation: { ref: "cnv_sales" },
      plan: applied,
      createdResourceRefs: ["agt_created"],
      receipt: {
        ref: "rcp_applied",
        planRef: applied.ref,
        planRevision: applied.revision,
        outcome: "APPLIED",
        operationReceipts: [],
        conflicts: [],
        auditRefs: [],
        createdResourceRefs: ["agt_created"],
        createdAt: "2026-08-28T00:02:00Z",
      },
    });
    const store = useAssistantStore();
    store.conversations = [conversation()];
    store.selectedRef = "cnv_sales";

    const receipt = await store.apply(plan());

    expect(receipt.outcome).toBe("APPLIED");
    expect(store.sortedConversations[0]?.updatedAt).toBe(
      "2026-08-28T00:00:00Z",
    );
    expect(store.selectedConversation?.title).toBe("Настройка отдела продаж");
    expect(store.selectedConversation?.turns[0]?.plan?.state).toBe("APPLIED");
    expect(store.selectedConversation?.turns[0]?.plan?.receipt).toEqual(
      receipt,
    );
  });

  function systemImageApplication() {
    const original = plan();
    const owner = {
      scopeKind: "ORGANIZATION",
      organizationRef: "org_synthetic",
      systemAssistantRef: "ast_system_assistant",
      recipeRef: "imgrec_synthetic",
    };
    const selected = original.operations[0];
    if (!selected) throw new Error("Plan fixture has no operation");
    const source: AssistantPlan = {
      ...original,
      projectRef: undefined,
      operations: [
        {
          ...selected,
          type: "UPDATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE",
          action: "UPDATE",
          target: {
            kind: "ROLE_IMAGE_RECIPE",
            ref: owner.recipeRef,
            name: "Собственный образ",
            version: 10,
          },
          expectedVersion: 10,
          parameters: owner,
          after: owner,
        },
      ],
    };
    const receipt: AssistantPlanReceipt = {
      ref: "rct_system_image",
      planRef: source.ref,
      planRevision: source.revision,
      outcome: "APPLIED",
      operationReceipts: [
        {
          operationRef: selected.ref,
          resourceRef: owner.recipeRef,
          outcome: "APPLIED",
          auditRef: "aud_image",
        },
      ],
      conflicts: [],
      auditRefs: [],
      createdResourceRefs: [owner.recipeRef],
      createdAt: "2026-10-05T18:00:00Z",
    };
    return {
      source,
      response: {
        conversation: { ref: source.conversationRef },
        plan: {
          ...source,
          version: source.version + 1,
          state: "APPLIED" as const,
          applied: true,
        },
        receipt,
        createdResourceRefs: [owner.recipeRef],
      },
    };
  }

  it("сразу связывает native SYSTEM UPDATE со сборкой через отдельную квитанцию ответа без reload", async () => {
    const { source, response } = systemImageApplication();
    expect(response.plan.receipt).toBeUndefined();
    applyPlanDraftMock.mockResolvedValue(response);
    const store = useAssistantStore();
    store.conversations = [conversation(source)];
    store.selectedRef = source.conversationRef;
    const receipt = await store.apply(source);
    const applied = store.selectedConversation?.turns[0]?.plan;
    expect(applied?.receipt).toEqual(receipt);
    expect(applied).toBeDefined();
    if (!applied) throw new Error("Applied plan was not retained");
    expect(
      assistantRoleImageBuildTarget(applied, "op_sales", "org_synthetic"),
    ).toEqual({
      resourceScope: { kind: "ORGANIZATION", organizationRef: "org_synthetic" },
      recipeRef: "imgrec_synthetic",
    });
    expect(
      assistantRoleImageBuildTarget(applied, "op_sales", "org_foreign"),
    ).toBeUndefined();
    expect(readConversationsMock).not.toHaveBeenCalled();
    expect(store.conversations.at(0)?.turns).toHaveLength(1);
  });

  it.each([
    "receipt-plan",
    "receipt-revision",
    "plan-revision",
    "plan-conversation",
    "outcome",
    "state",
  ])("не присоединяет неверно связанную квитанцию: %s", async (kind) => {
    const { source, response } = systemImageApplication();
    if (kind === "receipt-plan") response.receipt.planRef = "pln_foreign";
    if (kind === "receipt-revision") response.receipt.planRevision += 1;
    if (kind === "plan-revision") {
      response.plan.revision += 1;
      response.receipt.planRevision = response.plan.revision;
    }
    if (kind === "plan-conversation")
      response.plan.conversationRef = "cnv_foreign";
    if (kind === "outcome") response.receipt.outcome = "CONFLICT";
    const invalid =
      kind === "state"
        ? { ...response, plan: { ...response.plan, state: "VALID" as const } }
        : response;
    applyPlanDraftMock.mockResolvedValue(invalid);
    const store = useAssistantStore();
    store.conversations = [conversation(source)];
    store.selectedRef = source.conversationRef;
    await expect(store.apply(source)).rejects.toBeInstanceOf(AppProblem);
    expect(store.receipt).toBeUndefined();
    expect(store.selectedConversation?.turns[0]?.plan?.state).toBe("VALID");
    expect(store.selectedConversation?.turns[0]?.plan?.receipt).toBeUndefined();
    expect(readConversationsMock).not.toHaveBeenCalled();
  });

  it.each([false, true])(
    "new file revision native receipt missing=%s закрыто проверяется до APPLIED",
    async (missing) => {
      const source = { ...plan(), operations: [fileOperationFixture()] };
      const receipt = {
        ...requireFixture(appliedFilePlanFixture().receipt),
        planRef: source.ref,
        planRevision: source.revision,
      };
      if (missing)
        delete requireFixture(receipt.operationReceipts[0]).artifactRevision;
      applyPlanDraftMock.mockResolvedValue({
        conversation: { ref: source.conversationRef },
        plan: {
          ...source,
          state: "APPLIED",
          applied: true,
          version: source.version + 1,
        },
        receipt,
        createdResourceRefs: [],
      });
      const store = useAssistantStore();
      store.conversations = [conversation(source)];
      store.selectedRef = source.conversationRef;
      if (missing) {
        await expect(store.apply(source)).rejects.toBeInstanceOf(AppProblem);
        expect(store.receipt).toBeUndefined();
        expect(store.selectedConversation?.turns[0]?.plan?.state).toBe("VALID");
      } else {
        expect(await store.apply(source)).toEqual(receipt);
        expect(store.selectedConversation?.turns[0]?.plan?.receipt).toEqual(
          receipt,
        );
      }
    },
  );
});
