import { createPinia, setActivePinia } from "pinia";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type {
  IntegrationConnection,
  Project,
  ProjectPage,
  Run,
  RunEvent,
  RunWorkspace,
  SearchResult,
  SearchResultPage,
} from "@/shared/api/generated/openapi/types.gen";
import { selectedProjectRef, selectProjectRef } from "@/shared/project-context";

const listProjectsMock = vi.hoisted(() => vi.fn());
const searchPlatformMock = vi.hoisted(() => vi.fn());
const listAuditEventsMock = vi.hoisted(() => vi.fn());
const getRunGraphMock = vi.hoisted(() => vi.fn());
const listRunEventsMock = vi.hoisted(() => vi.fn());
const listAgentInstructionVersionsMock = vi.hoisted(() => vi.fn());
const downloadArtifactMock = vi.hoisted(() => vi.fn());
const createIntegrationConnectionMock = vi.hoisted(() => vi.fn());
const configureIntegrationConnectionCredentialMock = vi.hoisted(() => vi.fn());

vi.mock("@/shared/api/generated/openapi/sdk.gen", async (importOriginal) => ({
  ...(await importOriginal<
    typeof import("@/shared/api/generated/openapi/sdk.gen")
  >()),
  listProjects: listProjectsMock,
  searchPlatform: searchPlatformMock,
  listAuditEvents: listAuditEventsMock,
  getRunGraph: getRunGraphMock,
  listRunEvents: listRunEventsMock,
  listAgentInstructionVersions: listAgentInstructionVersionsMock,
  downloadArtifact: downloadArtifactMock,
  createIntegrationConnection: createIntegrationConnectionMock,
  configureIntegrationConnectionCredential:
    configureIntegrationConnectionCredentialMock,
}));
vi.mock("@/shared/api/client", () => ({
  requestSignal: () => new AbortController().signal,
}));

import { usePlatformStore } from "@/features/platform/store";

function project(ref: string, version = 1): Project {
  return {
    ref,
    version,
    name: ref,
    purpose: "Рабочий Проект",
    language: "ru",
    lifecycle: "ACTIVE",
    agentCount: 0,
    workflowCount: 0,
    activeRunCount: 0,
    pendingGateCount: 0,
    updatedAt: "2026-08-23T00:00:00Z",
    nextActions: [],
  };
}

function response(
  items: Project[],
  nextActions: ProjectPage["nextActions"] = [],
): {
  data: ProjectPage;
  response: Response;
} {
  return {
    data: { items, nextActions },
    response: new Response(null, { status: 200 }),
  };
}

function deferred<T>(): {
  promise: Promise<T>;
  resolve: (value: T) => void;
} {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((ready) => {
    resolve = ready;
  });
  return { promise, resolve };
}

function searchResponse(items: SearchResult[]): {
  data: SearchResultPage;
  response: Response;
} {
  return {
    data: { items },
    response: new Response(null, { status: 200 }),
  };
}

function searchResult(ref: string): SearchResult {
  return {
    kind: "PROJECT",
    ref,
    projectRef: ref,
    title: ref,
    subtitle: "Search result",
    state: "ACTIVE",
    updatedAt: "2026-08-23T00:00:00Z",
  };
}

function run(sequence: number): Run {
  return {
    ref: "run_consistent01",
    rootRunRef: "run_consistent01",
    projectRef: "project_owner",
    sessionRef: "session_consistent01",
    target: {
      type: "AGENT",
      ref: "agent_owner",
      displayName: "Координатор",
      version: 1,
    },
    title: "Согласованный запуск",
    source: "CONTROL_CENTER",
    initiator: { ref: "user_owner", displayName: "Владелец" },
    state: sequence >= 2 ? "WAITING_HUMAN" : "RUNNING",
    attempt: 1,
    version: sequence,
    graphRevision: sequence,
    lastEventSequence: sequence,
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
    incidents: [],
    createdAt: "2026-08-23T00:00:00Z",
    nextActions: [],
  };
}

function runEvent(sequence: number): RunEvent {
  return {
    ref: `event_${String(sequence).padStart(8, "0")}`,
    runRef: "run_consistent01",
    sequence,
    type: "RUN_STATE_CHANGED",
    summary: "Состояние изменено",
    occurredAt: "2026-08-23T00:00:00Z",
    graphRevision: sequence,
    run: {
      ref: "run_consistent01",
      version: sequence,
      state: sequence >= 2 ? "WAITING_HUMAN" : "RUNNING",
      graphRevision: sequence,
      lastEventSequence: sequence,
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
      nextActions: [],
    },
  };
}

function integrationConnection(
  version: number,
  credentialsConfigured = false,
): IntegrationConnection {
  return {
    ref: "connection_github",
    version,
    definitionKey: "github",
    name: "Основная организация",
    state: credentialsConfigured ? "CONNECTED" : "NOT_CONNECTED",
    credentialsConfigured,
    credentialsHint: credentialsConfigured ? "••••••••" : "Не настроены",
    capabilities: [],
    grants: [],
    nextActions: [],
    definitionVersion: "1.0.0",
    definitionDigest: "sha256:definition",
    publicConfiguration: { organization: "codex-k8s" },
  };
}

describe("platform store", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    listProjectsMock.mockReset();
    searchPlatformMock.mockReset();
    listAuditEventsMock.mockReset();
    getRunGraphMock.mockReset();
    listRunEventsMock.mockReset();
    listAgentInstructionVersionsMock.mockReset();
    downloadArtifactMock.mockReset();
    createIntegrationConnectionMock.mockReset();
    configureIntegrationConnectionCredentialMock.mockReset();
    selectProjectRef(undefined);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("не позволяет старому HTTP response перезаписать новый", async () => {
    const oldResponse = deferred<ReturnType<typeof response>>();
    const newResponse = deferred<ReturnType<typeof response>>();
    listProjectsMock
      .mockReturnValueOnce(oldResponse.promise)
      .mockReturnValueOnce(newResponse.promise);
    const store = usePlatformStore();

    const oldRequest = store.loadProjects();
    const newRequest = store.loadProjects();
    newResponse.resolve(response([project("project_new", 2)]));
    await newRequest;
    oldResponse.resolve(response([project("project_old")]));
    await oldRequest;

    expect(store.projectList.map((item) => item.ref)).toEqual(["project_new"]);
    expect(store.loading.projects).toBe(false);
  });

  it("не позволяет старому ответу поиска перезаписать новый", async () => {
    const oldResponse = deferred<ReturnType<typeof searchResponse>>();
    const newResponse = deferred<ReturnType<typeof searchResponse>>();
    searchPlatformMock
      .mockReturnValueOnce(oldResponse.promise)
      .mockReturnValueOnce(newResponse.promise);
    const store = usePlatformStore();

    const oldRequest = store.search("old result");
    const newRequest = store.search("new result");
    newResponse.resolve(searchResponse([searchResult("project_new")]));
    await newRequest;
    oldResponse.resolve(searchResponse([searchResult("project_old")]));
    await oldRequest;

    expect(store.searchResults.map((item) => item.ref)).toEqual([
      "project_new",
    ]);
    expect(store.loading.search).toBe(false);
  });

  it("заменяет authoritative collection и удаляет исчезнувший ресурс", async () => {
    listProjectsMock
      .mockResolvedValueOnce(
        response([project("project_first"), project("project_second")]),
      )
      .mockResolvedValueOnce(response([project("project_second", 2)]));
    const store = usePlatformStore();

    await store.loadProjects();
    await store.loadProjects();

    expect(Object.keys(store.projects)).toEqual(["project_second"]);
    expect(store.projects.project_second?.version).toBe(2);
  });

  it("передаёт поиск аудита авторитетному owner API", async () => {
    listAuditEventsMock.mockResolvedValue({
      data: { items: [] },
      response: new Response(null, { status: 200 }),
    });
    const store = usePlatformStore();

    await store.loadAudit("project_sales", "Квартальный отчёт");

    expect(listAuditEventsMock).toHaveBeenCalledWith(
      expect.objectContaining({
        query: {
          projectRef: "project_sales",
          query: "Квартальный отчёт",
          pageSize: 100,
        },
      }),
    );
  });

  it("собирает опубликованные revisions инструкций из bounded pages", async () => {
    listAgentInstructionVersionsMock
      .mockResolvedValueOnce({
        data: {
          items: [
            {
              ref: "ins_2",
              version: 2,
              revision: 2,
              state: "PUBLISHED",
              content: "Вторая",
              validationMessages: [],
              createdAt: "2026-08-27T00:00:00Z",
            },
          ],
          nextPageToken: "2",
        },
        response: new Response(null, { status: 200 }),
      })
      .mockResolvedValueOnce({
        data: {
          items: [
            {
              ref: "ins_1",
              version: 1,
              revision: 1,
              state: "PUBLISHED",
              content: "Первая",
              validationMessages: [],
              createdAt: "2026-08-26T00:00:00Z",
            },
          ],
        },
        response: new Response(null, { status: 200 }),
      });
    const store = usePlatformStore();

    await store.loadInstructionVersions("agt_owner");

    expect(
      store.instructionVersions.agt_owner?.map((item) => item.ref),
    ).toEqual(["ins_2", "ins_1"]);
    expect(listAgentInstructionVersionsMock).toHaveBeenNthCalledWith(
      2,
      expect.objectContaining({ query: { pageSize: 100, pageToken: "2" } }),
    );
  });

  it("загружает Run и граф из одного snapshot и не принимает более новые события", async () => {
    const workspace: RunWorkspace = {
      run: run(2),
      graph: {
        runRef: "run_consistent01",
        revision: 2,
        sequence: 2,
        nodes: [],
        edges: [],
      },
    };
    getRunGraphMock.mockResolvedValue({
      data: workspace,
      response: new Response(null, { status: 200 }),
    });
    listRunEventsMock.mockResolvedValue({
      data: { items: [runEvent(1), runEvent(2), runEvent(3)] },
      response: new Response(null, { status: 200 }),
    });
    const store = usePlatformStore();

    await store.loadRun("run_consistent01");

    expect(store.runs.run_consistent01).toEqual(workspace.run);
    expect(store.graphs.run_consistent01).toEqual(workspace.graph);
    expect(Object.keys(store.events.run_consistent01 ?? {})).toEqual([
      "1",
      "2",
    ]);
  });

  it("сохраняет authoritative Run и граф при временной ошибке event catch-up", async () => {
    const workspace: RunWorkspace = {
      run: run(2),
      graph: {
        runRef: "run_consistent01",
        revision: 2,
        sequence: 2,
        nodes: [],
        edges: [],
      },
    };
    getRunGraphMock.mockResolvedValue({
      data: workspace,
      response: new Response(null, { status: 200 }),
    });
    listRunEventsMock.mockResolvedValue({
      error: {
        status: 503,
        code: "RUN_EVENTS_UNAVAILABLE",
        title: "История событий временно недоступна",
        retryable: true,
      },
      response: new Response(null, { status: 503 }),
    });
    const store = usePlatformStore();

    await store.loadRun("run_consistent01");

    expect(store.runs.run_consistent01).toEqual(workspace.run);
    expect(store.graphs.run_consistent01).toEqual(workspace.graph);
    expect(store.problems.run).toMatchObject({
      code: "RUN_EVENTS_UNAVAILABLE",
      kind: "unavailable",
    });
    expect(store.loading.run).toBe(false);
  });

  it("заменяет разрешённые действия коллекции только авторитетным ответом", async () => {
    listProjectsMock
      .mockResolvedValueOnce(
        response([project("project_owner")], ["CREATE_PROJECT"]),
      )
      .mockResolvedValueOnce(response([project("project_member")]));
    const store = usePlatformStore();

    await store.loadProjects();
    expect(store.projectCollectionActions).toEqual(["CREATE_PROJECT"]);

    await store.loadProjects();
    expect(store.projectCollectionActions).toEqual([]);
  });

  it("сохраняет forbidden как безопасное состояние запроса", async () => {
    listProjectsMock.mockResolvedValue({
      error: {
        status: 403,
        code: "PROJECT_ACCESS_DENIED",
        correlationId: "correlation_safe",
      },
      response: new Response(null, { status: 403 }),
    });
    const store = usePlatformStore();

    await store.loadProjects();

    expect(store.problems.projects?.kind).toBe("forbidden");
    expect(store.problems.projects?.code).toBe("PROJECT_ACCESS_DENIED");
    expect(store.loading.projects).toBe(false);
  });

  it("повторяет безопасное чтение artifact после временного сетевого сбоя", async () => {
    const expected = new Blob(["artifact"]);
    downloadArtifactMock
      .mockResolvedValueOnce({
        error: { status: 0, code: "UNKNOWN", retryable: true },
      })
      .mockResolvedValueOnce({
        data: expected,
        response: new Response(null, { status: 200 }),
      });
    const store = usePlatformStore();

    await expect(
      store.downloadArtifactContent("artifact_owner", "DOWNLOAD"),
    ).resolves.toBe(expected);
    expect(downloadArtifactMock).toHaveBeenCalledTimes(2);
  });

  it("очищает owner state и project context при завершении сессии", async () => {
    listProjectsMock.mockResolvedValue(response([project("project_owner")]));
    const store = usePlatformStore();
    await store.loadProjects();
    selectProjectRef("project_owner");

    store.clearOwnerState();

    expect(store.projectList).toEqual([]);
    expect(store.projectCollectionActions).toEqual([]);
    expect(selectedProjectRef()).toBeUndefined();
  });

  it("настраивает credential отдельной versioned-командой и хранит только masked readback", async () => {
    vi.stubGlobal("document", {
      cookie: `__Host-kodex-csrf=${"a".repeat(43)}`,
    });
    const created = integrationConnection(4);
    const configured = integrationConnection(5, true);
    createIntegrationConnectionMock.mockResolvedValue({
      data: created,
      response: new Response(null, { status: 201 }),
    });
    configureIntegrationConnectionCredentialMock.mockResolvedValue({
      data: configured,
      response: new Response(null, { status: 200 }),
    });
    const store = usePlatformStore();
    const rawCredential = "test-only-secret-value";

    const metadata = await store.connectIntegration({
      definitionKey: "github",
      name: "Основная организация",
      publicConfiguration: { organization: "codex-k8s" },
    });
    const result = await store.configureConnectionCredential(
      metadata,
      rawCredential,
      "credential-request-key",
    );

    expect(result).toEqual(configured);
    expect(configureIntegrationConnectionCredentialMock).toHaveBeenCalledWith(
      expect.objectContaining({
        path: { connectionRef: created.ref },
        body: { value: rawCredential },
        headers: {
          "If-Match": '"4"',
          "Idempotency-Key": "credential-request-key",
          "X-CSRF-Token": "a".repeat(43),
        },
      }),
    );
    expect(store.connections[created.ref]).toEqual(configured);
    expect(JSON.stringify(store.connections)).not.toContain(rawCredential);
  });

  it("сохраняет созданное подключение при временной ошибке credential-шагa", async () => {
    vi.stubGlobal("document", {
      cookie: `__Host-kodex-csrf=${"a".repeat(43)}`,
    });
    const created = integrationConnection(4);
    createIntegrationConnectionMock.mockResolvedValue({
      data: created,
      response: new Response(null, { status: 201 }),
    });
    configureIntegrationConnectionCredentialMock.mockResolvedValue({
      error: {
        status: 503,
        code: "CREDENTIAL_STORE_UNAVAILABLE",
        retryable: true,
      },
      response: new Response(null, { status: 503 }),
    });
    const store = usePlatformStore();

    const metadata = await store.connectIntegration({
      definitionKey: "github",
      name: "Основная организация",
    });
    await expect(
      store.configureConnectionCredential(
        metadata,
        "test-only-secret-value",
        "credential-request-key",
      ),
    ).rejects.toMatchObject({
      code: "CREDENTIAL_STORE_UNAVAILABLE",
      retryable: true,
    });

    expect(store.connections[created.ref]).toEqual(created);
  });
});
