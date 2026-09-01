import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { RuntimeSecret, RuntimeSecretPage } from "./model";

const api = vi.hoisted(() => ({
  createRuntimeSecret: vi.fn(),
  loadRuntimeSecretPage: vi.fn(),
  normalizeRuntimeSecretProblem: vi.fn((error: unknown) => error),
  revokeRuntimeSecret: vi.fn(),
  rotateRuntimeSecret: vi.fn(),
}));
vi.mock("./api", () => api);
vi.mock("@/shared/api/client", () => ({
  requestSignal: () => new AbortController().signal,
}));

import { useRuntimeSecretsStore } from "./store";

const secret: RuntimeSecret = {
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
    setActivePinia(createPinia());
    vi.clearAllMocks();
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
});
