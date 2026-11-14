import { effectScope } from "vue";
import { afterEach, describe, expect, it, vi } from "vitest";
import { resetOwnerRequests } from "@/shared/api/owner-lifetime";
import type { ModelDiscoveryPage } from "@/shared/api/generated/openapi/types.gen";
import { useModelDiscovery } from "./model-discovery";

const api = vi.hoisted(() => ({ loadModelDiscovery: vi.fn() }));
vi.mock("./model-discovery-api", () => api);

function page(): ModelDiscoveryPage {
  return {
    items: [],
    groupBy: "PROVIDER",
    nextPageToken: "",
    total: "0",
    source: {
      schema: "kodex.model-provider-discovery/v1",
      source: "models.dev",
      license: "MIT",
      sourceRevision: "a".repeat(40),
      upstreamSchema: "models.dev/experimental-v2.0",
      schemaSha256: "b".repeat(64),
      upstreamSha256: "c".repeat(64),
      snapshotSha256: "d".repeat(64),
      sourceUrl: "https://models.dev/_experimental_v2.0_api.json",
      modelCount: 9726,
      retrievedAt: "2026-11-14T04:03:01Z",
      projectionSchema: "urn:kodex:contract:model-provider-discovery:v1",
      projectionSchemaSha256: "e".repeat(64),
      upstreamSchemaValidation: "NOT_PERFORMED",
      overlaySha256: "e".repeat(64),
      catalogSha256: "f".repeat(64),
    },
  };
}

afterEach(() => {
  vi.clearAllMocks();
  resetOwnerRequests();
});

describe("справочный каталог моделей", () => {
  it("отбрасывает старый ответ после нового поиска", async () => {
    let finish!: (value: ModelDiscoveryPage) => void;
    api.loadModelDiscovery.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    );
    api.loadModelDiscovery.mockResolvedValueOnce({ ...page(), total: "2" });
    const scope = effectScope();
    const state = scope.run(() => useModelDiscovery());
    if (!state) throw new Error("Synthetic scope unavailable");
    const old = state.search();
    state.query.value = "glm";
    await state.search();
    finish({ ...page(), total: "99" });
    await old;
    expect(state.total.value).toBe("2");
    scope.stop();
  });

  it("смена владельца очищает provenance и не применяет поздний ACK", async () => {
    let finish!: (value: ModelDiscoveryPage) => void;
    api.loadModelDiscovery.mockResolvedValueOnce(page());
    const scope = effectScope();
    const state = scope.run(() => useModelDiscovery());
    if (!state) throw new Error("Synthetic scope unavailable");
    await state.search();
    expect(state.source.value).toBeDefined();
    api.loadModelDiscovery.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    );
    const pending = state.search();
    resetOwnerRequests();
    finish({ ...page(), total: "99" });
    await pending;
    expect(state.source.value).toBeUndefined();
    expect(state.loaded.value).toBe(false);
    expect(state.failed.value).toBe(false);
    expect(state.total.value).toBe("0");
    scope.stop();
  });

  it("пагинация сохраняет выполненный запрос при редактировании поля поиска", async () => {
    api.loadModelDiscovery.mockResolvedValueOnce({
      ...page(),
      nextPageToken: "pinned-next",
    });
    api.loadModelDiscovery.mockResolvedValueOnce(page());
    const scope = effectScope();
    const state = scope.run(() => useModelDiscovery());
    if (!state) throw new Error("Synthetic scope unavailable");
    state.query.value = "glm";
    state.filters.family = "glm";
    state.filters.groupBy = "FAMILY";
    await state.search();
    state.query.value = "other";
    state.filters.family = "gpt";
    state.filters.groupBy = "PROVIDER";
    await state.search(true);
    expect(api.loadModelDiscovery.mock.calls[1]?.[3]).toMatchObject({
      family: "glm",
      groupBy: "FAMILY",
    });
    expect(api.loadModelDiscovery.mock.calls[1]?.slice(0, 2)).toEqual([
      "glm",
      "pinned-next",
    ]);
    scope.stop();
  });
});
