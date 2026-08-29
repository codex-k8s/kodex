import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type {
  AgentRuntimeConfigurationView,
  RuntimeEnvironmentPage,
  RuntimeEnvironmentVersionPage,
} from "@/shared/api/generated/openapi/types.gen";

const getAgentRuntimeConfigurationMock = vi.hoisted(() => vi.fn());
const listRuntimeEnvironmentSetsMock = vi.hoisted(() => vi.fn());
const listRuntimeEnvironmentVersionsMock = vi.hoisted(() => vi.fn());

vi.mock("@/shared/api/generated/openapi/sdk.gen", async (importOriginal) => ({
  ...(await importOriginal<
    typeof import("@/shared/api/generated/openapi/sdk.gen")
  >()),
  getAgentRuntimeConfiguration: getAgentRuntimeConfigurationMock,
  listRuntimeEnvironmentSets: listRuntimeEnvironmentSetsMock,
  listRuntimeEnvironmentVersions: listRuntimeEnvironmentVersionsMock,
}));
vi.mock("@/shared/api/client", () => ({
  requestSignal: () => new AbortController().signal,
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
    listRuntimeEnvironmentSetsMock.mockReset();
    listRuntimeEnvironmentVersionsMock.mockReset();
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

  it("добавляет следующую cursor-страницу ревизий без повторов", async () => {
    const currentVersion = {
      ref: "environment_version_2",
      version: 2,
      revision: 2,
      values: [],
      secretDescriptors: [],
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
