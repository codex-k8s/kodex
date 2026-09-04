import { createPinia, setActivePinia } from "pinia";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type {
  Artifact,
  AuditEvent,
  IntegrationConnection,
  IntegrationDefinition,
  Project,
  ProjectPage,
  Run,
  RunPage,
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
const listRunsMock = vi.hoisted(() => vi.fn());
const listAgentInstructionVersionsMock = vi.hoisted(() => vi.fn());
const downloadArtifactMock = vi.hoisted(() => vi.fn());
const getArtifactMock = vi.hoisted(() =>
  vi.fn<typeof import("@/shared/api/generated/openapi/sdk.gen").getArtifact>(),
);
const getArtifactImpactMock = vi.hoisted(() =>
  vi.fn<
    typeof import("@/shared/api/generated/openapi/sdk.gen").getArtifactImpact
  >(),
);
const deleteArtifactMock = vi.hoisted(() =>
  vi.fn<
    typeof import("@/shared/api/generated/openapi/sdk.gen").deleteArtifact
  >(),
);
const createIntegrationConnectionMock = vi.hoisted(() => vi.fn());
const configureIntegrationConnectionCredentialMock = vi.hoisted(() => vi.fn());
const updateIntegrationConnectionMock = vi.hoisted(() =>
  vi.fn<
    typeof import("@/shared/api/generated/openapi/sdk.gen").updateIntegrationConnection
  >(),
);
const deleteIntegrationConnectionMock = vi.hoisted(() =>
  vi.fn<
    typeof import("@/shared/api/generated/openapi/sdk.gen").deleteIntegrationConnection
  >(),
);
const listIntegrationDefinitionsMock = vi.hoisted(() => vi.fn());
const listIntegrationConnectionsMock = vi.hoisted(() => vi.fn());
const uploadArtifactMock = vi.hoisted(() =>
  vi.fn<
    typeof import("@/shared/api/generated/openapi/sdk.gen").uploadArtifact
  >(),
);
const uploadOrganizationArtifactMock = vi.hoisted(() =>
  vi.fn<
    typeof import("@/shared/api/generated/openapi/sdk.gen").uploadOrganizationArtifact
  >(),
);

vi.mock("@/shared/api/generated/openapi/sdk.gen", async (importOriginal) => ({
  ...(await importOriginal<
    typeof import("@/shared/api/generated/openapi/sdk.gen")
  >()),
  listProjects: listProjectsMock,
  searchPlatform: searchPlatformMock,
  listAuditEvents: listAuditEventsMock,
  getRunGraph: getRunGraphMock,
  listRunEvents: listRunEventsMock,
  listRuns: listRunsMock,
  listAgentInstructionVersions: listAgentInstructionVersionsMock,
  downloadArtifact: downloadArtifactMock,
  getArtifact: getArtifactMock,
  getArtifactImpact: getArtifactImpactMock,
  deleteArtifact: deleteArtifactMock,
  createIntegrationConnection: createIntegrationConnectionMock,
  configureIntegrationConnectionCredential:
    configureIntegrationConnectionCredentialMock,
  updateIntegrationConnection: updateIntegrationConnectionMock,
  deleteIntegrationConnection: deleteIntegrationConnectionMock,
  listIntegrationDefinitions: listIntegrationDefinitionsMock,
  listIntegrationConnections: listIntegrationConnectionsMock,
  uploadArtifact: uploadArtifactMock,
  uploadOrganizationArtifact: uploadOrganizationArtifactMock,
}));
vi.mock("@/shared/api/client", () => ({
  requestSignal: (parent?: AbortSignal) =>
    parent ?? new AbortController().signal,
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
    data: { items, total: items.length },
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
    titleSource: "USER_EDITED",
    activitySummary: "Выполняется согласованный запуск",
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

function integrationDefinition(): IntegrationDefinition {
  return {
    key: "github",
    name: "GitHub",
    description: "Репозитории и задачи",
    category: "source-control",
    builtIn: true,
    available: true,
    capabilities: [],
    configurationFields: [],
    schemaVersion: "integrations.kodex.io/v1",
    definitionVersion: "1.0.0",
    origin: "SHIPPED",
    digest: "a".repeat(64),
    adapter: "GITHUB",
  };
}

function artifact(ref: string, projectRef?: string): Artifact {
  return {
    ref,
    version: 1,
    ...(projectRef ? { projectRef } : {}),
    fileName: `${ref}.txt`,
    mediaType: "text/plain",
    sizeBytes: 7,
    digest: `sha256:${"a".repeat(64)}`,
    scanState: "CLEAN",
    source: "INTERACTION_ATTACHMENT",
    revision: 1,
    lifecycleState: "ACTIVE",
    agentBindings: [],
    previewAvailable: true,
    createdAt: "2026-08-29T00:00:00Z",
    nextActions: [],
  };
}

function auditEvent(ref: string, occurredAt: string): AuditEvent {
  return {
    ref,
    projectRef: "project_sales",
    initiator: { ref: "user_owner", displayName: "Владелец" },
    executor: "CONTROL_CENTER",
    source: "CONTROL_CENTER",
    action: "schedule.create",
    resourceType: "SCHEDULE",
    resourceRef: "schedule_quarterly",
    resourceName: "Квартальный отчёт",
    outcome: "SUCCEEDED",
    safeSummary: "Автоматизация создана",
    occurredAt,
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
    listRunsMock.mockReset();
    listAgentInstructionVersionsMock.mockReset();
    downloadArtifactMock.mockReset();
    getArtifactMock.mockReset();
    getArtifactImpactMock.mockReset();
    deleteArtifactMock.mockReset();
    createIntegrationConnectionMock.mockReset();
    configureIntegrationConnectionCredentialMock.mockReset();
    updateIntegrationConnectionMock.mockReset();
    deleteIntegrationConnectionMock.mockReset();
    listIntegrationDefinitionsMock.mockReset();
    listIntegrationConnectionsMock.mockReset();
    uploadArtifactMock.mockReset();
    uploadOrganizationArtifactMock.mockReset();
    selectProjectRef(undefined);
  });

  afterEach(() => {
    vi.useRealTimers();
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

  it("обновляет список запусков по authoritative readback без замены существующего объекта", async () => {
    const first = run(1);
    const second = run(2);
    const runResponse = (
      items: Run[],
    ): { data: RunPage; response: Response } => ({
      data: { items },
      response: new Response(null, { status: 200 }),
    });
    listRunsMock
      .mockResolvedValueOnce(runResponse([first]))
      .mockResolvedValueOnce(runResponse([second]));
    const store = usePlatformStore();

    await store.loadRuns("project_owner");
    const original = store.runs.run_consistent01;
    await store.loadRuns("project_owner");

    expect(store.runs.run_consistent01).toBe(original);
    expect(store.runs.run_consistent01?.version).toBe(2);
    expect(store.runs.run_consistent01?.state).toBe("WAITING_HUMAN");
  });

  it("передаёт поиск аудита авторитетному owner API", async () => {
    listAuditEventsMock.mockResolvedValue({
      data: { items: [], nextPageToken: "" },
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

  it("добавляет audit cursor-страницу без повторов и зацикливания", async () => {
    const first = auditEvent("aud_first_page", "2026-08-31T12:00:00Z");
    const second = auditEvent("aud_second_page", "2026-08-31T11:00:00Z");
    listAuditEventsMock
      .mockResolvedValueOnce({
        data: { items: [first], nextPageToken: "audit-page-2" },
        response: new Response(null, { status: 200 }),
      })
      .mockResolvedValueOnce({
        data: { items: [first, second], nextPageToken: "audit-page-2" },
        response: new Response(null, { status: 200 }),
      });
    const store = usePlatformStore();

    await store.loadAudit("project_sales", " Квартальный отчёт ");
    await Promise.all([
      store.loadMoreAudit("project_sales", " Квартальный отчёт "),
      store.loadMoreAudit("project_sales", " Квартальный отчёт "),
    ]);

    expect(listAuditEventsMock).toHaveBeenCalledTimes(2);
    expect(listAuditEventsMock).toHaveBeenLastCalledWith(
      expect.objectContaining({
        query: {
          projectRef: "project_sales",
          query: "Квартальный отчёт",
          pageSize: 100,
          pageToken: "audit-page-2",
        },
      }),
    );
    expect(store.auditEvents.map((event) => event.ref)).toEqual([
      first.ref,
      second.ref,
    ]);
    expect(store.auditNextPageToken).toBeUndefined();
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
      data: {
        items: [runEvent(1), runEvent(2), runEvent(3)],
        currentSequence: 3,
        complete: true,
      },
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

  it("не публикует новый Run и граф при временной ошибке event catch-up", async () => {
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

    expect(store.runs.run_consistent01).toBeUndefined();
    expect(store.graphs.run_consistent01).toBeUndefined();
    expect(store.problems.run).toMatchObject({
      code: "RUN_EVENTS_UNAVAILABLE",
      kind: "unavailable",
    });
    expect(store.loading.run).toBe(false);
  });

  it("пагинирует события до sequence авторитетного graph snapshot", async () => {
    const workspace: RunWorkspace = {
      run: run(3),
      graph: {
        runRef: "run_consistent01",
        revision: 3,
        sequence: 3,
        nodes: [],
        edges: [],
      },
    };
    getRunGraphMock.mockResolvedValue({
      data: workspace,
      response: new Response(null, { status: 200 }),
    });
    listRunEventsMock
      .mockResolvedValueOnce({
        data: {
          items: [runEvent(1), runEvent(2)],
          currentSequence: 3,
          complete: false,
        },
        response: new Response(null, { status: 200 }),
      })
      .mockResolvedValueOnce({
        data: {
          items: [runEvent(3)],
          currentSequence: 3,
          complete: true,
        },
        response: new Response(null, { status: 200 }),
      });
    const store = usePlatformStore();

    await store.loadRun("run_consistent01");

    expect(listRunEventsMock).toHaveBeenNthCalledWith(
      2,
      expect.objectContaining({ query: { afterSequence: 2, limit: 500 } }),
    );
    expect(Object.keys(store.events.run_consistent01 ?? {})).toEqual([
      "1",
      "2",
      "3",
    ]);
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

  it("восстанавливает безопасную загрузку после временного сетевого сбоя", async () => {
    vi.useFakeTimers();
    listProjectsMock
      .mockResolvedValueOnce({
        error: { status: 0, code: "UNKNOWN", retryable: true },
      })
      .mockResolvedValueOnce(response([project("project_recovered")]));
    const store = usePlatformStore();

    const loading = store.loadProjects();
    await vi.runAllTimersAsync();
    await loading;

    expect(listProjectsMock).toHaveBeenCalledTimes(2);
    expect(store.projectList.map((item) => item.ref)).toEqual([
      "project_recovered",
    ]);
    expect(store.problems.projects).toBeUndefined();
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

  it("выбирает область загрузки вложения по наличию Проекта", async () => {
    vi.stubGlobal("document", {
      cookie: `__Host-kodex-csrf=${"a".repeat(43)}`,
    });
    const organizationArtifact = artifact("artifact_organization");
    const projectArtifact = artifact("artifact_project", "project_owner");
    uploadOrganizationArtifactMock.mockResolvedValue({
      data: organizationArtifact,
      request: new Request("http://localhost/api/v1/artifacts"),
      response: new Response(null, { status: 201 }),
    });
    uploadArtifactMock.mockResolvedValue({
      data: projectArtifact,
      request: new Request(
        "http://localhost/api/v1/projects/project_owner/artifacts",
      ),
      response: new Response(null, { status: 201 }),
    });
    const store = usePlatformStore();

    const organizationFile = new File(["global"], "global.txt", {
      type: "text/plain",
    });
    const projectFile = new File(["project"], "project.txt", {
      type: "text/plain",
    });
    const organizationController = new AbortController();
    const projectController = new AbortController();
    await store.uploadAttachmentArtifact(
      undefined,
      organizationFile,
      organizationController.signal,
    );
    await store.uploadAttachmentArtifact(
      "project_owner",
      projectFile,
      projectController.signal,
    );

    const organizationCall = uploadOrganizationArtifactMock.mock.calls[0]?.[0];
    expect(organizationCall?.body).toBe(organizationFile);
    expect(organizationCall?.headers["X-File-Name"]).toBe("global.txt");
    expect(organizationCall?.signal).toBe(organizationController.signal);
    const projectCall = uploadArtifactMock.mock.calls[0]?.[0];
    expect(projectCall?.path).toEqual({ projectRef: "project_owner" });
    expect(projectCall?.body).toBe(projectFile);
    expect(projectCall?.headers["X-File-Name"]).toBe("project.txt");
    expect(projectCall?.signal).toBe(projectController.signal);
    expect(store.artifacts.artifact_organization).toEqual(organizationArtifact);
    expect(store.artifacts.artifact_project).toEqual(projectArtifact);
  });

  it("читает avatar artifact и перемещает его в общую корзину с OCC", async () => {
    vi.stubGlobal("document", {
      cookie: `__Host-kodex-csrf=${"a".repeat(43)}`,
    });
    const active = {
      ...artifact("art_avatar01", "project_owner"),
      fileName: "agent-avatar.png",
      mediaType: "image/png",
      nextActions: ["DELETE" as never],
    };
    const deleted = {
      ...active,
      version: 2,
      lifecycleState: "DELETED" as const,
      nextActions: [],
    };
    getArtifactMock.mockResolvedValue({
      data: active,
      error: undefined,
      response: new Response(null, { status: 200 }),
    });
    getArtifactImpactMock.mockResolvedValue({
      data: {
        action: "DELETE",
        activeRuns: [],
        activeRunsTruncated: false,
        activeRuntimeCount: 0,
        artifactRef: active.ref,
        artifactVersion: active.version,
        attachmentCount: 0,
        bindingCount: 0,
        blockers: [],
        impactDigest: "d".repeat(64),
        permitted: true,
      },
      error: undefined,
      response: new Response(null, { status: 200 }),
    });
    deleteArtifactMock.mockResolvedValue({
      data: deleted,
      error: undefined,
      response: new Response(null, { status: 200 }),
    });
    const store = usePlatformStore();

    await expect(store.readArtifact(active.ref)).resolves.toEqual(active);
    await expect(store.deleteProjectArtifact(active)).resolves.toEqual(deleted);

    expect(getArtifactMock).toHaveBeenCalledWith(
      expect.objectContaining({ path: { artifactRef: active.ref } }),
    );
    expect(getArtifactImpactMock).toHaveBeenCalledWith(
      expect.objectContaining({
        path: { artifactRef: active.ref },
        query: { action: "DELETE" },
      }),
    );
    const deleteCall = deleteArtifactMock.mock.calls[0]?.[0];
    expect(deleteCall?.path).toEqual({ artifactRef: active.ref });
    expect(deleteCall?.headers["If-Match"]).toBe('"1"');
    expect(deleteCall?.headers["X-Impact-Digest"]).toBe("d".repeat(64));
    expect(store.artifacts[active.ref]?.lifecycleState).toBe("DELETED");
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

  it("сохраняет авторитетную готовность core независимо от списка подключений", async () => {
    const definition = integrationDefinition();
    const connection = integrationConnection(3, true);
    listIntegrationDefinitionsMock.mockResolvedValue({
      data: {
        items: [definition],
        coreReady: true,
        nextActions: ["CREATE_CONNECTION"],
        nextPageToken: "",
      },
      response: new Response(null, { status: 200 }),
    });
    listIntegrationConnectionsMock.mockResolvedValue({
      data: { items: [connection], nextPageToken: "" },
      response: new Response(null, { status: 200 }),
    });
    const store = usePlatformStore();

    await store.loadIntegrations();

    expect(store.integrationCoreReady).toBe(true);
    expect(store.definitions[definition.key]).toEqual(definition);
    expect(store.connections[connection.ref]).toEqual(connection);
    expect(store.integrationDefinitionActions).toEqual(["CREATE_CONNECTION"]);

    store.clearOwnerState();
    expect(store.integrationCoreReady).toBeUndefined();
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

  it("изменяет и удаляет подключение отдельными OCC-командами без credential value", async () => {
    vi.stubGlobal("document", {
      cookie: `__Host-kodex-csrf=${"a".repeat(43)}`,
    });
    const current = {
      ...integrationConnection(4, true),
      nextActions: ["UPDATE", "DELETE"] as IntegrationConnection["nextActions"],
    };
    const updated = {
      ...current,
      version: 5,
      name: "GitHub для тестов",
      publicConfiguration: { organization: "codex-k8s-fixtures" },
    };
    const deleted = {
      ...updated,
      version: 6,
      state: "DELETED" as const,
      nextActions: [],
    };
    updateIntegrationConnectionMock.mockResolvedValue({
      data: updated,
      error: undefined,
      response: new Response(null, { status: 200 }),
    });
    deleteIntegrationConnectionMock.mockResolvedValue({
      data: deleted,
      error: undefined,
      response: new Response(null, { status: 200 }),
    });
    const store = usePlatformStore();

    const updateResult = await store.updateConnection(current, {
      name: updated.name,
      publicConfiguration: updated.publicConfiguration,
    });
    const deleteResult = await store.deleteConnection(updateResult);
    const updateHeaders =
      updateIntegrationConnectionMock.mock.calls[0]?.[0].headers;
    const deleteHeaders =
      deleteIntegrationConnectionMock.mock.calls[0]?.[0].headers;

    expect(updateIntegrationConnectionMock).toHaveBeenCalledWith(
      expect.objectContaining({
        path: { connectionRef: current.ref },
        body: {
          name: updated.name,
          publicConfiguration: updated.publicConfiguration,
        },
        headers: {
          "If-Match": '"4"',
          "Idempotency-Key": updateHeaders?.["Idempotency-Key"],
          "X-CSRF-Token": "a".repeat(43),
        },
      }),
    );
    expect(deleteIntegrationConnectionMock).toHaveBeenCalledWith(
      expect.objectContaining({
        path: { connectionRef: current.ref },
        headers: {
          "If-Match": '"5"',
          "Idempotency-Key": deleteHeaders?.["Idempotency-Key"],
          "X-CSRF-Token": "a".repeat(43),
        },
      }),
    );
    expect(updateHeaders?.["Idempotency-Key"]).toHaveLength(36);
    expect(deleteHeaders?.["Idempotency-Key"]).toHaveLength(36);
    expect(deleteResult).toEqual(deleted);
    expect(store.connections[current.ref]).toEqual(deleted);
    expect(JSON.stringify(store.connections)).not.toContain(
      "test-only-secret-value",
    );
  });
});
