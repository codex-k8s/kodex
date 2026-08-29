import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type {
  AgentRuntimeConfigurationView,
  RuntimeEnvironmentPage,
  RuntimeEnvironmentVersionPage,
} from "@/shared/api/generated/openapi/types.gen";

const getAgentRuntimeConfigurationMock = vi.hoisted(() => vi.fn());
const createRuntimeEnvironmentSetMock = vi.hoisted(() => vi.fn());
const getRoleImageRecipeMock = vi.hoisted(() => vi.fn());
const listRoleImageRecipesMock = vi.hoisted(() => vi.fn());
const listRuntimeEnvironmentSetsMock = vi.hoisted(() => vi.fn());
const listRuntimeEnvironmentVersionsMock = vi.hoisted(() => vi.fn());
const publishRuntimeEnvironmentVersionMock = vi.hoisted(() => vi.fn());
const mutateMock = vi.hoisted(() => vi.fn());

const runtimeImage = {
  artifactRef: "imgart_main",
  recipeRef: "imgrec_main",
  recipeGeneration: 1,
  reference: "registry.example/runtime@sha256:" + "f".repeat(64),
  digest: "f".repeat(64),
};

vi.mock("@/shared/api/generated/openapi/sdk.gen", async (importOriginal) => ({
  ...(await importOriginal<
    typeof import("@/shared/api/generated/openapi/sdk.gen")
  >()),
  createRuntimeEnvironmentSet: createRuntimeEnvironmentSetMock,
  getAgentRuntimeConfiguration: getAgentRuntimeConfigurationMock,
  getRoleImageRecipe: getRoleImageRecipeMock,
  listRoleImageRecipes: listRoleImageRecipesMock,
  listRuntimeEnvironmentSets: listRuntimeEnvironmentSetsMock,
  listRuntimeEnvironmentVersions: listRuntimeEnvironmentVersionsMock,
  publishRuntimeEnvironmentVersion: publishRuntimeEnvironmentVersionMock,
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
        digest: "e".repeat(64),
        createdAt: "2026-08-28T08:00:00Z",
      },
      updatedAt: "2026-08-28T08:00:00Z",
    },
    safeEffectiveConfig: `model = "${model}"`,
    agentVersion: version,
  };
}

function response<T>(data: T): { data: T; response: Response } {
  return { data, response: new Response(null, { status: 200 }) };
}

describe("runtime store", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    getAgentRuntimeConfigurationMock.mockReset();
    createRuntimeEnvironmentSetMock.mockReset();
    getRoleImageRecipeMock.mockReset();
    listRoleImageRecipesMock.mockReset();
    listRuntimeEnvironmentSetsMock.mockReset();
    listRuntimeEnvironmentVersionsMock.mockReset();
    publishRuntimeEnvironmentVersionMock.mockReset();
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
});
