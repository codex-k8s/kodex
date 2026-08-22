import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type {
  Project,
  ProjectPage,
} from "@/shared/api/generated/openapi/types.gen";
import { selectedProjectRef, selectProjectRef } from "@/shared/project-context";

const listProjectsMock = vi.hoisted(() => vi.fn());

vi.mock("@/shared/api/generated/openapi/sdk.gen", async (importOriginal) => ({
  ...(await importOriginal<
    typeof import("@/shared/api/generated/openapi/sdk.gen")
  >()),
  listProjects: listProjectsMock,
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

describe("platform store", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    listProjectsMock.mockReset();
    selectProjectRef(undefined);
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
});
