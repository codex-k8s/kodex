import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type {
  Agent,
  AgentPage,
} from "@/shared/api/generated/openapi/types.gen";

const api = vi.hoisted(() => ({ loadAgentCatalogPage: vi.fn() }));
vi.mock("./api", () => api);

import { useAgentCatalogStore } from "./store";

function agent(ref: string, version = 1): Agent {
  return {
    ref,
    version,
    projectRef: "project_sales",
    name: ref,
    purpose: "Проверять данные",
    roleDescription: "Работает с фактами",
    state: "READY",
    enabled: true,
    system: false,
    runtimeRef: "runtime_standard",
    runtimeName: "Стандартный runtime",
    runtimeReady: true,
    capabilities: [],
    integrations: [],
    knowledgeArtifactRefs: [],
    updatedAt: "2026-08-30T10:00:00Z",
    nextActions: [],
  };
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((ready) => {
    resolve = ready;
  });
  return { promise, resolve };
}

describe("agent catalog store", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    vi.clearAllMocks();
  });

  it("не позволяет устаревшему серверному поиску перезаписать новый", async () => {
    const first = deferred<AgentPage>();
    const second = deferred<AgentPage>();
    api.loadAgentCatalogPage
      .mockReturnValueOnce(first.promise)
      .mockReturnValueOnce(second.promise);
    const store = useAgentCatalogStore();

    const oldLoad = store.load("project_sales", "old");
    const newLoad = store.load("project_sales", "new");
    second.resolve({ items: [agent("agent_new")] });
    await newLoad;
    first.resolve({ items: [agent("agent_old")] });
    await oldLoad;

    expect(store.items.map((item) => item.ref)).toEqual(["agent_new"]);
  });

  it("добавляет cursor-страницу, обновляет дубли и сохраняет query", async () => {
    api.loadAgentCatalogPage
      .mockResolvedValueOnce({
        items: [agent("agent_first")],
        nextPageToken: "page_2",
      })
      .mockResolvedValueOnce({
        items: [agent("agent_first", 2), agent("agent_second")],
      });
    const store = useAgentCatalogStore();

    await store.load("project_sales", "аналитик");
    await store.loadMore();

    expect(store.items.map((item) => [item.ref, item.version])).toEqual([
      ["agent_first", 2],
      ["agent_second", 1],
    ]);
    expect(api.loadAgentCatalogPage).toHaveBeenLastCalledWith({
      projectRef: "project_sales",
      query: "аналитик",
      pageToken: "page_2",
    });
    expect(store.hasMore).toBe(false);
  });

  it("не зацикливается на повторённом server cursor", async () => {
    api.loadAgentCatalogPage.mockResolvedValue({
      items: [agent("agent_first")],
      nextPageToken: "page_2",
    });
    const store = useAgentCatalogStore();

    await store.load("project_sales");
    await store.loadMore();
    await store.loadMore();

    expect(api.loadAgentCatalogPage).toHaveBeenCalledTimes(2);
    expect(store.hasMore).toBe(false);
  });
  it("realtime сохраняет карточку до свежего ответа и отклоняет прежнюю cursor страницу", async () => {
    api.loadAgentCatalogPage.mockResolvedValueOnce({
      items: [agent("agent_first")],
      nextPageToken: "next",
    });
    const store = useAgentCatalogStore();
    await store.load("project_sales");
    const old = deferred<AgentPage>();
    api.loadAgentCatalogPage.mockReturnValueOnce(old.promise);
    const oldPage = store.loadMore();
    store.prepareRefresh(true);
    expect(store.items[0]?.version).toBe(1);
    expect(store.hasMore).toBe(false);
    const fresh = deferred<AgentPage>();
    api.loadAgentCatalogPage.mockReturnValueOnce(fresh.promise);
    const refresh = store.load("project_sales", "", true);
    expect(store.items[0]?.version).toBe(1);
    old.resolve({ items: [agent("agent_stale")], nextPageToken: "stale" });
    await oldPage;
    expect(store.items.map((item) => item.ref)).toEqual(["agent_first"]);
    fresh.resolve({ items: [agent("agent_first", 2)] });
    await refresh;
    expect(store.items[0]?.version).toBe(2);
    store.prepareRefresh(false);
    expect(store.items).toEqual([]);
    store.clear();
    expect(store.loading).toBe(false);
  });
});
