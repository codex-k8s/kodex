import { createPinia, setActivePinia } from "pinia";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type {
  AgentRuntimeConfigurationView,
  RuntimeEnvironmentPolicy,
  RuntimeEnvironmentPage,
  RuntimeEnvironmentSet,
  RuntimeEnvironmentVersionPage,
} from "@/shared/api/generated/openapi/types.gen";
import { defaultRuntimeEnvironmentPolicy } from "@/features/runtime/environment-form";

const getAgentRuntimeConfigurationMock = vi.hoisted(() => vi.fn());
const listAgentRuntimeConfigurationVersionsMock = vi.hoisted(() => vi.fn());
const createRuntimeEnvironmentSetMock = vi.hoisted(() => vi.fn());
const deleteRuntimeEnvironmentMock = vi.hoisted(() => vi.fn());
const getRuntimeEnvironmentReadinessMock = vi.hoisted(() => vi.fn());
const getRoleImageRecipeMock = vi.hoisted(() => vi.fn());
const listRuntimeEnvironmentAgentsMock = vi.hoisted(() => vi.fn());
const listRoleImageRecipesMock = vi.hoisted(() => vi.fn());
const listRuntimeEnvironmentSetsMock = vi.hoisted(() => vi.fn());
const listRuntimeEnvironmentVersionsMock = vi.hoisted(() => vi.fn());
const publishRuntimeEnvironmentVersionMock = vi.hoisted(() => vi.fn());
const rollbackRuntimeEnvironmentMock = vi.hoisted(() => vi.fn());
const setRuntimeEnvironmentEnabledMock = vi.hoisted(() => vi.fn());
const mutateMock = vi.hoisted(() => vi.fn());

const runtimeImage = {
  artifactRef: "imgart_main",
  recipeRef: "imgrec_main",
  recipeGeneration: 1,
  reference: "registry.example/runtime@sha256:" + "f".repeat(64),
  digest: "f".repeat(64),
};

const runtimePolicy = {
  resources: defaultRuntimeEnvironmentPolicy().resources,
  volumes: [],
  network: {
    denyByDefault: true,
    egress: [
      { destination: "DNS", protocol: "TCP", port: 53 },
      { destination: "DNS", protocol: "UDP", port: 53 },
      { destination: "PROVIDER_PROXY", protocol: "TCP", port: 8080 },
      { destination: "RUNTIME_CALLBACK", protocol: "TCP", port: 8444 },
    ],
  },
  kubernetesAccess: { kind: "NONE", namespace: "kodex-runtime" },
  resourcesDigest: "1".repeat(64),
  volumesDigest: "2".repeat(64),
  networkDigest: "3".repeat(64),
  rbacDigest: "4".repeat(64),
} satisfies RuntimeEnvironmentPolicy;

vi.mock("@/shared/api/generated/openapi/sdk.gen", async (importOriginal) => ({
  ...(await importOriginal<
    typeof import("@/shared/api/generated/openapi/sdk.gen")
  >()),
  createRuntimeEnvironmentSet: createRuntimeEnvironmentSetMock,
  deleteRuntimeEnvironment: deleteRuntimeEnvironmentMock,
  getAgentRuntimeConfiguration: getAgentRuntimeConfigurationMock,
  getRuntimeEnvironmentReadiness: getRuntimeEnvironmentReadinessMock,
  getRoleImageRecipe: getRoleImageRecipeMock,
  listRoleImageRecipes: listRoleImageRecipesMock,
  listAgentRuntimeConfigurationVersions:
    listAgentRuntimeConfigurationVersionsMock,
  listRuntimeEnvironmentAgents: listRuntimeEnvironmentAgentsMock,
  listRuntimeEnvironmentSets: listRuntimeEnvironmentSetsMock,
  listRuntimeEnvironmentVersions: listRuntimeEnvironmentVersionsMock,
  publishRuntimeEnvironmentVersion: publishRuntimeEnvironmentVersionMock,
  rollbackRuntimeEnvironment: rollbackRuntimeEnvironmentMock,
  setRuntimeEnvironmentEnabled: setRuntimeEnvironmentEnabledMock,
}));
vi.mock("@/shared/api/client", () => ({
  requestSignal: () => new AbortController().signal,
}));
vi.mock("@/shared/api/mutation", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/shared/api/mutation")>()),
  mutate: mutateMock,
}));

import { useRuntimeStore } from "@/features/runtime/store";

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

function view(model: string, version: number): AgentRuntimeConfigurationView {
  return {
    configuration: {
      ref: `rconf_${String(version)}`,
      version,
      agentRef: "agent_sales",
      runtimeProfileRef: "runtime_standard",
      provider: "openai-codex",
      model,
      providerPolicy: {
        ref: `policy_${String(version)}`,
        version,
        mode: "FIXED",
        accountCandidates: [{ accountRef: "account_main", weight: 1 }],
        digest: "a".repeat(64),
        createdAt: "2026-08-28T08:00:00Z",
      },
      digest: "b".repeat(64),
      createdAt: "2026-08-28T08:00:00Z",
    },
    publishedOverlay: {
      ref: "overlay_published",
      version,
      revision: version,
      state: "PUBLISHED",
      content: "",
      digest: "c".repeat(64),
      validationMessages: [],
      createdAt: "2026-08-28T08:00:00Z",
    },
    environmentBinding: {
      ref: "binding_main",
      version,
      agentRef: "agent_sales",
      environmentRef: "environment_main",
      digest: "d".repeat(64),
    },
    environment: {
      ref: "environment_main",
      version,
      projectRef: "project_sales",
      name: "Основное окружение",
      description: "Для продаж",
      state: "ACTIVE",
      currentVersion: {
        ref: "environment_version_main",
        version,
        revision: version,
        values: [],
        secretDescriptors: [],
        image: runtimeImage,
        tools: [],
        policy: runtimePolicy,
        digest: "e".repeat(64),
        createdAt: "2026-08-28T08:00:00Z",
      },
      updatedAt: "2026-08-28T08:00:00Z",
      ready: true,
      readinessBlockers: [],
      nextActions: ["OPEN", "UPDATE", "DISABLE", "DELETE"],
    },
    safeEffectiveConfig: `model = "${model}"`,
    agentVersion: version,
  };
}

function response<T>(data: T): { data: T; response: Response } {
  return { data, response: new Response(null, { status: 200 }) };
}

describe("runtime store", () => {
  afterEach(() => vi.useRealTimers());

  beforeEach(() => {
    setActivePinia(createPinia());
    getAgentRuntimeConfigurationMock.mockReset();
    listAgentRuntimeConfigurationVersionsMock.mockReset();
    createRuntimeEnvironmentSetMock.mockReset();
    deleteRuntimeEnvironmentMock.mockReset();
    getRoleImageRecipeMock.mockReset();
    getRuntimeEnvironmentReadinessMock.mockReset();
    listRoleImageRecipesMock.mockReset();
    listRuntimeEnvironmentSetsMock.mockReset();
    listRuntimeEnvironmentAgentsMock.mockReset();
    listRuntimeEnvironmentVersionsMock.mockReset();
    publishRuntimeEnvironmentVersionMock.mockReset();
    rollbackRuntimeEnvironmentMock.mockReset();
    setRuntimeEnvironmentEnabledMock.mockReset();
    mutateMock.mockReset();
    mutateMock.mockImplementation(
      async (request: (headers: Record<string, string>) => Promise<unknown>) =>
        request({
          "Idempotency-Key": "idem_1",
          "If-Match": '"3"',
          "X-CSRF-Token": "csrf_1",
        }),
    );
  });

  it("не позволяет старому runtime readback перезаписать новый", async () => {
    const oldRequest = deferred<ReturnType<typeof response>>();
    const newRequest = deferred<ReturnType<typeof response>>();
    getAgentRuntimeConfigurationMock
      .mockReturnValueOnce(oldRequest.promise)
      .mockReturnValueOnce(newRequest.promise);
    const store = useRuntimeStore();

    const oldLoad = store.loadAgentRuntime("agent_sales");
    const newLoad = store.loadAgentRuntime("agent_sales");
    newRequest.resolve(response(view("gpt-new", 2)));
    await newLoad;
    oldRequest.resolve(response(view("gpt-old", 1)));
    await oldLoad;

    expect(store.agentViews.agent_sales?.configuration.model).toBe("gpt-new");
  });

  it("повторяет безопасное чтение runtime-конфигурации и версий в расширенном бюджете", async () => {
    vi.useFakeTimers();
    const transient = new Error("Failed to fetch");
    const authoritative = view("gpt-5.6-sol", 5);
    getAgentRuntimeConfigurationMock
      .mockRejectedValueOnce(transient)
      .mockRejectedValueOnce(transient)
      .mockRejectedValueOnce(transient)
      .mockRejectedValueOnce(transient)
      .mockResolvedValueOnce(response(authoritative));
    listAgentRuntimeConfigurationVersionsMock
      .mockRejectedValueOnce(transient)
      .mockRejectedValueOnce(transient)
      .mockRejectedValueOnce(transient)
      .mockRejectedValueOnce(transient)
      .mockResolvedValueOnce(
        response({ items: [authoritative.configuration] }),
      );
    const store = useRuntimeStore();

    const runtimeLoad = store.loadAgentRuntime("agent_sales");
    await vi.runAllTimersAsync();
    await runtimeLoad;
    const versionsLoad = store.loadAgentVersions("agent_sales");
    await vi.runAllTimersAsync();
    await versionsLoad;

    expect(getAgentRuntimeConfigurationMock).toHaveBeenCalledTimes(5);
    expect(listAgentRuntimeConfigurationVersionsMock).toHaveBeenCalledTimes(5);
    expect(store.agentViews.agent_sales).toEqual(authoritative);
    expect(store.agentVersions.agent_sales).toEqual([
      authoritative.configuration,
    ]);
  });

  it("передаёт серверу поиск и cursor без локальной подмены каталога", async () => {
    const page: RuntimeEnvironmentPage = {
      items: [],
      nextPageToken: "cursor-next",
    };
    listRuntimeEnvironmentSetsMock.mockResolvedValue(response(page));
    const store = useRuntimeStore();

    await expect(
      store.searchEnvironmentPage("project_sales", "pdf", "cursor-current"),
    ).resolves.toEqual(page);
    expect(listRuntimeEnvironmentSetsMock).toHaveBeenCalledWith(
      expect.objectContaining({
        path: { projectRef: "project_sales" },
        query: {
          query: "pdf",
          pageSize: 30,
          pageToken: "cursor-current",
        },
      }),
    );
  });

  it("выбирает только promoted images и читает verified tools exact artifact", async () => {
    const environment = {
      environmentKey: "standard",
      dockerfile: "FROM registry.example/base@sha256:" + "a".repeat(64),
    };
    listRoleImageRecipesMock
      .mockResolvedValueOnce(
        response({
          items: [
            {
              ref: "imgrec_draft",
              version: 1,
              projectRef: "project_sales",
              roleDefinitionRef: "role_sales",
              name: "Черновик",
              state: "ACTIVE",
              environment,
              generation: 1,
              promotedImageReady: false,
              createdAt: "2026-08-29T10:00:00Z",
              updatedAt: "2026-08-29T10:00:00Z",
              nextActions: [],
            },
          ],
          nextPageToken: "cursor-2",
        }),
      )
      .mockResolvedValueOnce(
        response({
          items: [
            {
              ref: "imgrec_main",
              version: 2,
              projectRef: "project_sales",
              roleDefinitionRef: "role_sales",
              name: "Инструменты продаж",
              state: "ACTIVE",
              environment,
              generation: 2,
              promotedImageReady: true,
              activeImageArtifactRef: "imgart_main",
              promotedImageReference: runtimeImage.reference,
              createdAt: "2026-08-29T10:00:00Z",
              updatedAt: "2026-08-29T11:00:00Z",
              nextActions: [],
            },
          ],
        }),
      );
    getRoleImageRecipeMock.mockResolvedValueOnce(
      response({
        recipe: {},
        builds: [],
        activeArtifact: {
          ref: "imgart_main",
          version: 1,
          recipeRef: "imgrec_main",
          recipeGeneration: 2,
          manifestDigest: "f".repeat(64),
          promotedReference: runtimeImage.reference,
          admissionVerdict: "ACCEPTED",
          tools: [{ name: "gh", version: "2.80.0" }],
          promotedAt: "2026-08-29T11:00:00Z",
        },
      }),
    );
    const store = useRuntimeStore();

    const page = await store.searchPromotedRoleImagePage(
      "project_sales",
      "продаж",
    );
    expect(page.items).toEqual([
      expect.objectContaining({
        ref: "imgart_main",
        recipeRef: "imgrec_main",
      }),
    ]);
    await expect(
      store.loadPromotedRoleImageArtifact(
        "project_sales",
        "imgrec_main",
        "imgart_main",
      ),
    ).resolves.toEqual(
      expect.objectContaining({ tools: [{ name: "gh", version: "2.80.0" }] }),
    );
  });

  it("обязательно передаёт exact image и tools при create и publish", async () => {
    const input = {
      name: "Окружение продаж",
      description: "Работа с GitHub",
      imageArtifactRef: "imgart_main",
      tools: [
        {
          name: "GitHub CLI",
          command: "gh",
          description: "Работа с разрешёнными репозиториями",
          usageHint: "Используйте только в границах задачи.",
        },
      ],
      values: [],
      secretBindings: [
        {
          name: "GITHUB_TOKEN",
          secretRef: "secret_github_token",
        },
      ],
      policy: defaultRuntimeEnvironmentPolicy(),
    };
    const environment = view("gpt-5.6-sol", 3).environment;
    createRuntimeEnvironmentSetMock.mockResolvedValueOnce(
      response(environment),
    );
    publishRuntimeEnvironmentVersionMock.mockResolvedValueOnce(
      response({ ...environment, version: 4 }),
    );
    const store = useRuntimeStore();

    await store.createEnvironment("project_sales", input);
    await store.publishEnvironment(environment, input);

    const createRequest: unknown =
      createRuntimeEnvironmentSetMock.mock.calls[0]?.[0];
    const publishRequest: unknown =
      publishRuntimeEnvironmentVersionMock.mock.calls[0]?.[0];
    expect(createRequest).toMatchObject({ body: input });
    expect(publishRequest).toMatchObject({
      body: input,
      headers: { "If-Match": '"3"' },
    });
    expect(JSON.stringify({ createRequest, publishRequest })).not.toMatch(
      /secretName|secretKey|secretUid|secretResourceVersion|contentSha256/,
    );
  });

  it("добавляет следующую cursor-страницу ревизий без повторов", async () => {
    const currentVersion = {
      ref: "environment_version_2",
      version: 2,
      revision: 2,
      values: [],
      secretDescriptors: [],
      image: runtimeImage,
      tools: [],
      policy: runtimePolicy,
      digest: "a".repeat(64),
      createdAt: "2026-08-29T12:00:00Z",
    };
    const first: RuntimeEnvironmentVersionPage = {
      items: [currentVersion],
      nextPageToken: "cursor-2",
    };
    const second: RuntimeEnvironmentVersionPage = {
      items: [
        currentVersion,
        {
          ref: "environment_version_1",
          version: 1,
          revision: 1,
          values: [],
          secretDescriptors: [],
          image: runtimeImage,
          tools: [],
          policy: runtimePolicy,
          digest: "b".repeat(64),
          createdAt: "2026-08-29T11:00:00Z",
        },
      ],
    };
    listRuntimeEnvironmentVersionsMock
      .mockResolvedValueOnce(response(first))
      .mockResolvedValueOnce(response(second));
    const store = useRuntimeStore();

    await store.loadEnvironmentVersions("environment_main");
    await store.loadEnvironmentVersions("environment_main", false);

    expect(
      store.environmentVersions.environment_main?.map((item) => item.ref),
    ).toEqual(["environment_version_2", "environment_version_1"]);
    expect(listRuntimeEnvironmentVersionsMock).toHaveBeenLastCalledWith(
      expect.objectContaining({
        path: { environmentRef: "environment_main" },
        query: { pageSize: 30, pageToken: "cursor-2" },
      }),
    );
  });

  it("rollback публикует выбранную immutable revision как новую", async () => {
    const environment = view("gpt-5.6-sol", 3).environment;
    const restored = {
      ...environment,
      version: 4,
      currentVersion: {
        ...environment.currentVersion,
        ref: "environment_version_restored",
        revision: 4,
      },
    };
    rollbackRuntimeEnvironmentMock.mockResolvedValueOnce(response(restored));
    const store = useRuntimeStore();

    await expect(
      store.restoreEnvironment(environment, "environment_version_1"),
    ).resolves.toEqual(restored);
    expect(rollbackRuntimeEnvironmentMock).toHaveBeenCalledWith(
      expect.objectContaining({
        path: { environmentRef: environment.ref },
        body: { publishedVersionRef: "environment_version_1" },
        headers: {
          "Idempotency-Key": "idem_1",
          "If-Match": '"3"',
          "X-CSRF-Token": "csrf_1",
        },
      }),
    );
  });

  it("выполняет lifecycle с OCC и загружает exact readiness и bindings", async () => {
    const environment = view("gpt-5.6-sol", 3).environment;
    const disabled: RuntimeEnvironmentSet = {
      ...environment,
      version: 4,
      state: "DISABLED",
      nextActions: ["OPEN", "ENABLE", "DELETE"],
    };
    setRuntimeEnvironmentEnabledMock.mockReturnValueOnce(response(disabled));
    getRuntimeEnvironmentReadinessMock.mockReturnValueOnce(
      response({
        environmentRef: environment.ref,
        environmentVersion: disabled.version,
        publishedVersionRef: disabled.currentVersion.ref,
        publishedVersionDigest: disabled.currentVersion.digest,
        ready: false,
        blockers: ["environment_disabled"],
        observedAt: "2026-08-30T10:00:00Z",
      }),
    );
    listRuntimeEnvironmentAgentsMock.mockReturnValueOnce(
      response({ items: [], nextPageToken: "" }),
    );
    deleteRuntimeEnvironmentMock.mockReturnValueOnce(
      response({
        ...disabled,
        version: 5,
        state: "DELETED" as const,
        nextActions: [],
      }),
    );
    const store = useRuntimeStore();

    await store.setEnvironmentEnabled(environment, false);
    await store.loadEnvironmentAgents(environment.ref);
    await store.removeEnvironment(disabled);

    expect(setRuntimeEnvironmentEnabledMock).toHaveBeenCalledWith(
      expect.objectContaining({
        path: { environmentRef: environment.ref },
        body: { enabled: false },
        headers: {
          "Idempotency-Key": "idem_1",
          "If-Match": '"3"',
          "X-CSRF-Token": "csrf_1",
        },
      }),
    );
    expect(store.environmentReadiness[environment.ref]).toMatchObject({
      environmentVersion: 4,
      ready: false,
    });
    expect(listRuntimeEnvironmentAgentsMock).toHaveBeenCalledWith(
      expect.objectContaining({
        path: { environmentRef: environment.ref },
        query: { pageSize: 30 },
      }),
    );
    expect(deleteRuntimeEnvironmentMock).toHaveBeenCalledWith(
      expect.objectContaining({
        path: { environmentRef: environment.ref },
        headers: {
          "Idempotency-Key": "idem_1",
          "If-Match": '"3"',
          "X-CSRF-Token": "csrf_1",
        },
      }),
    );
  });
});
