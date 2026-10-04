import { initializeRuntimeOwnerFixture } from "@/test-utils/runtime-owner-fixture";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { RuntimeSecret, RuntimeSecretPage } from "./model";

const api = vi.hoisted(() => ({
  createRuntimeSecret: vi.fn(),
  loadRuntimeSecretPage: vi.fn(),
  readRuntimeSecret: vi.fn(),
  normalizeRuntimeSecretProblem: vi.fn((error: unknown) => error),
  revokeRuntimeSecret: vi.fn(),
  rotateRuntimeSecret: vi.fn(),
}));
vi.mock("./api", () => api);
vi.mock("@/shared/api/client", () => ({
  requestSignal: () => new AbortController().signal,
}));

import { useRuntimeSecretsStore } from "./store";
import { usePlatformStore } from "@/features/platform/store";
import type { BootstrapState } from "@/shared/api/generated/openapi/types.gen";

const secret: RuntimeSecret = {
  scopeKind: "PROJECT",
  organizationRef: "org_synthetic",
  ref: "secret_main",
  version: 3,
  projectRef: "project_sales",
  name: "CRM_TOKEN",
  description: "Токен CRM",
  valueType: "STRING",
  state: "ACTIVE",
  currentRevision: 2,
  displayHint: { prefix: "tok", suffix: "9z" },
  nextActions: ["ROTATE", "REVOKE", "REVEAL"],
  createdAt: "2026-08-29T08:00:00Z",
  updatedAt: "2026-08-29T09:00:00Z",
};

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

describe("runtime secrets store", () => {
  beforeEach(() => {
    initializeRuntimeOwnerFixture("org_synthetic");
    vi.resetAllMocks();
    api.normalizeRuntimeSecretProblem.mockImplementation((error) => error);
    api.readRuntimeSecret.mockResolvedValue(secret);
  });
  it("не принимает PROJECT catalog без bootstrap или с foreign owner", () => {
    const store = useRuntimeSecretsStore();
    store.prepareRealtimeScope(secret.projectRef);
    expect(() =>
      store.applySnapshot(secret.projectRef, [
        { ...secret, organizationRef: "org_foreign" },
      ]),
    ).toThrow();
    usePlatformStore().bootstrap = undefined;
    expect(() => store.applySnapshot(secret.projectRef, [secret])).toThrow(
      "anchor",
    );
    expect(store.items).toEqual([]);
  });
  it("сбрасывает cache и не принимает позднюю страницу после owner flip", async () => {
    const pending = deferred<RuntimeSecretPage>();
    api.loadRuntimeSecretPage.mockReturnValue(pending.promise);
    const store = useRuntimeSecretsStore();
    const request = store.load(secret.projectRef);
    usePlatformStore().bootstrap = {
      organizationRef: "org_foreign",
    } as BootstrapState;
    pending.resolve({
      items: [{ ...secret, organizationRef: "org_foreign" }],
      nextPageToken: "",
    });
    await request;
    expect(store.items).toEqual([]);
    expect(store.nextPageToken).toBe("");
  });

  it("не позволяет устаревшему поиску перезаписать новый", async () => {
    const first = deferred<RuntimeSecretPage>();
    const second = deferred<RuntimeSecretPage>();
    api.loadRuntimeSecretPage
      .mockReturnValueOnce(first.promise)
      .mockReturnValueOnce(second.promise);
    const store = useRuntimeSecretsStore();

    const oldLoad = store.load("project_sales", "old");
    const newLoad = store.load("project_sales", "new");
    second.resolve({ items: [{ ...secret, name: "NEW" }], nextPageToken: "" });
    await newLoad;
    first.resolve({ items: [{ ...secret, name: "OLD" }], nextPageToken: "" });
    await oldLoad;

    expect(store.items.map((item) => item.name)).toEqual(["NEW"]);
  });

  it("добавляет cursor-страницу без дублей", async () => {
    api.loadRuntimeSecretPage
      .mockResolvedValueOnce({ items: [secret], nextPageToken: "page_2" })
      .mockResolvedValueOnce({
        items: [
          { ...secret, version: 4 },
          { ...secret, ref: "secret_second", name: "SECOND" },
        ],
        nextPageToken: "",
      });
    const store = useRuntimeSecretsStore();

    await store.load("project_sales", "crm");
    await store.loadMore();

    expect(store.items).toHaveLength(2);
    expect(store.items[0]?.version).toBe(4);
    expect(api.loadRuntimeSecretPage).toHaveBeenLastCalledWith(
      "project_sales",
      "crm",
      "page_2",
      expect.any(AbortSignal),
      20,
    );
  });

  it("не сохраняет plaintext create в Pinia state", async () => {
    api.loadRuntimeSecretPage.mockResolvedValue({
      items: [secret],
      nextPageToken: "",
    });
    api.createRuntimeSecret.mockResolvedValue(secret);
    const store = useRuntimeSecretsStore();
    await store.load("project_sales");

    await store.create({
      name: "CRM_TOKEN",
      description: "",
      valueType: "STRING",
      value: "must-never-enter-store",
    });

    expect(JSON.stringify(store.$state)).not.toContain(
      "must-never-enter-store",
    );
  });

  it("сохраняет receipt при отставании списка после создания", async () => {
    api.loadRuntimeSecretPage.mockResolvedValue({
      items: [],
      nextPageToken: "",
    });
    api.createRuntimeSecret.mockResolvedValue({
      ...secret,
      value: "private-value",
    });
    const store = useRuntimeSecretsStore();
    await store.load(secret.projectRef);
    await store.create({
      name: secret.name,
      description: "",
      valueType: "STRING",
      value: "private-value",
    });
    expect(store.items).toEqual([secret]);
    expect(JSON.stringify(store.$state)).not.toContain("private-value");
    expect(api.createRuntimeSecret).toHaveBeenCalledTimes(1);
  });

  it("не откатывает подтверждённую ротацию отставшим realtime-снимком", async () => {
    const rotated = { ...secret, version: 4, currentRevision: 3 };
    api.loadRuntimeSecretPage.mockResolvedValue({
      items: [secret],
      nextPageToken: "",
    });
    api.rotateRuntimeSecret.mockResolvedValue(rotated);
    const store = useRuntimeSecretsStore();
    await store.load(secret.projectRef);
    await store.rotate(secret, {
      valueType: "STRING",
      value: "private-rotation",
    });
    store.applySnapshot(secret.projectRef, [secret]);
    expect(store.items).toEqual([rotated]);
    expect(api.readRuntimeSecret).not.toHaveBeenCalled();
    expect(api.rotateRuntimeSecret).toHaveBeenCalledTimes(1);
  });

  it("не перечитывает список после мутации", async () => {
    const rotated = { ...secret, version: 4, currentRevision: 3 };
    api.loadRuntimeSecretPage.mockResolvedValue({
      items: [secret],
      nextPageToken: "",
    });
    api.rotateRuntimeSecret.mockResolvedValue(rotated);
    const store = useRuntimeSecretsStore();

    await store.load(secret.projectRef, "", 22);
    await store.rotate(secret, {
      valueType: "STRING",
      value: "private-rotation",
    });

    expect(api.loadRuntimeSecretPage).toHaveBeenCalledOnce();
    expect(api.loadRuntimeSecretPage).toHaveBeenCalledWith(
      secret.projectRef,
      "",
      undefined,
      expect.any(AbortSignal),
      22,
    );
    expect(api.readRuntimeSecret).not.toHaveBeenCalled();
  });

  it("отклоняет каталог другого проекта", async () => {
    api.loadRuntimeSecretPage.mockResolvedValue({
      items: [secret],
      nextPageToken: "",
    });
    const store = useRuntimeSecretsStore();
    await store.load("project_other");
    expect(store.items).toEqual([]);
    expect(store.problem).toBeDefined();
  });

  it("не повторяет незавершённую мутацию", async () => {
    const pending = deferred<RuntimeSecret>();
    api.loadRuntimeSecretPage.mockResolvedValue({
      items: [],
      nextPageToken: "",
    });
    api.createRuntimeSecret.mockReturnValue(pending.promise);
    const store = useRuntimeSecretsStore();
    await store.load(secret.projectRef);
    const input = {
      name: secret.name,
      description: "",
      valueType: "STRING" as const,
      value: "private-value",
    };
    const first = store.create(input);
    await expect(store.create(input)).rejects.toThrow("already in progress");
    pending.resolve(secret);
    await first;
    expect(api.createRuntimeSecret).toHaveBeenCalledTimes(1);
  });

  it("не сбрасывает busy нового экрана поздним ответом после dispose", async () => {
    const old = deferred<RuntimeSecret>();
    const next = deferred<RuntimeSecret>();
    api.loadRuntimeSecretPage.mockResolvedValue({
      items: [],
      nextPageToken: "",
    });
    api.createRuntimeSecret
      .mockReturnValueOnce(old.promise)
      .mockReturnValueOnce(next.promise);
    const store = useRuntimeSecretsStore();
    await store.load(secret.projectRef);
    const input = {
      name: secret.name,
      description: "",
      valueType: "STRING" as const,
      value: "private-value",
    };
    const first = store.create(input);
    store.dispose();
    await store.load(secret.projectRef);
    const second = store.create(input);
    old.resolve(secret);
    await first;
    expect(store.busyRef).toBe("create");
    next.resolve(secret);
    await second;
    expect(store.busyRef).toBe("");
  });

  it("сохраняет receipt без вторичного чтения списка", async () => {
    api.loadRuntimeSecretPage.mockResolvedValueOnce({
      items: [],
      nextPageToken: "",
    });
    api.createRuntimeSecret.mockResolvedValue(secret);
    const store = useRuntimeSecretsStore();
    await store.load(secret.projectRef);
    await store.create({
      name: secret.name,
      description: "",
      valueType: "STRING",
      value: "private-value",
    });
    expect(store.items).toEqual([secret]);
    expect(store.problem).toBeUndefined();
    expect(store.mutationProblem).toBeUndefined();
    expect(api.loadRuntimeSecretPage).toHaveBeenCalledOnce();
  });

  it("отклоняет повтор курсора и сохраняет уже загруженные данные", async () => {
    api.loadRuntimeSecretPage.mockResolvedValue({
      items: [secret],
      nextPageToken: "same",
    });
    const store = useRuntimeSecretsStore();
    await store.load(secret.projectRef);
    await store.loadMore();
    expect(store.items).toEqual([secret]);
    expect(store.problem).toBeDefined();
    expect(store.loadingMore).toBe(false);
  });
});
