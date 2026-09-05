import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ModelCapability } from "@/shared/api/generated/openapi/types.gen";
import { catalogStatusFixture } from "@/test-utils/runtime-catalog-fixture";
const sdk = vi.hoisted(() => ({
  listModelCapabilities:
    vi.fn<
      (request: { query?: unknown; signal: AbortSignal }) => Promise<unknown>
    >(),
}));
vi.mock("@/shared/api/generated/openapi/sdk.gen", () => sdk);
vi.mock("@/shared/api/client", () => ({
  requestSignal: (signal: AbortSignal) => signal,
}));
import {
  accountModelAvailable,
  loadModelCatalog,
  resolveAccountModel,
} from "./model-catalog";

const model: ModelCapability = {
  id: "model-current",
  providerDefinitionKey: "openai-codex",
  reasoningEfforts: ["medium", "high"],
  defaultReasoningEffort: "medium",
  available: true,
  eligibleProviderAccountRefs: ["pacc_primary"],
  readinessBlockers: [],
};
const response = (items: ModelCapability[], nextPageToken = "") => ({
  data: {
    items,
    nextPageToken,
    total: items.length,
    catalogRevision: `mcat_${"a".repeat(64)}`,
    catalogDigest: "a".repeat(64),
    catalogStatus: catalogStatusFixture,
  },
  response: new Response(null, { status: 200 }),
});
describe("model catalog", () => {
  beforeEach(() => vi.clearAllMocks());
  it("передаёт account/search/cursor и отмену серверному каталогу", async () => {
    sdk.listModelCapabilities.mockResolvedValue(response([model]));
    const signal = new AbortController().signal;
    await loadModelCatalog(
      "openai-codex",
      "pacc_primary",
      "  model  ",
      "next",
      signal,
      response([]).data,
    );
    expect(sdk.listModelCapabilities).toHaveBeenCalledWith({
      query: {
        providerDefinitionKey: "openai-codex",
        providerAccountRef: "pacc_primary",
        query: "model",
        pageToken: "next",
        pageSize: 40,
        expectedCatalogRevision: response([]).data.catalogRevision,
        expectedCatalogDigest: response([]).data.catalogDigest,
      },
      signal,
    });
  });
  it("отклоняет пропавший или изменившийся снимок между страницами", async () => {
    const first = response([], "next");
    sdk.listModelCapabilities
      .mockResolvedValueOnce(first)
      .mockResolvedValueOnce({
        ...response([model]),
        data: {
          ...response([model]).data,
          catalogRevision: `mcat_${"b".repeat(64)}`,
          catalogDigest: "b".repeat(64),
        },
      });
    await expect(
      resolveAccountModel(
        "openai-codex",
        "pacc_primary",
        model.id,
        new AbortController().signal,
      ),
    ).rejects.toThrow("snapshot mismatch");
    expect(sdk.listModelCapabilities.mock.lastCall?.[0].query).toMatchObject({
      expectedCatalogRevision: first.data.catalogRevision,
      expectedCatalogDigest: first.data.catalogDigest,
    });
    await expect(
      loadModelCatalog(
        "openai-codex",
        "pacc_primary",
        "",
        "next",
        new AbortController().signal,
      ),
    ).rejects.toThrow("pinned snapshot");
    sdk.listModelCapabilities.mockResolvedValue({
      ...first,
      data: { ...first.data, catalogDigest: undefined },
    });
    await expect(
      loadModelCatalog(
        "openai-codex",
        "pacc_primary",
        "",
        undefined,
        new AbortController().signal,
      ),
    ).rejects.toThrow("snapshot mismatch");
  });
  it("разрешает точный выбранный ID за первой страницей, не подменяя похожим", async () => {
    sdk.listModelCapabilities
      .mockResolvedValueOnce(
        response([{ ...model, id: "model-current-other" }], "next"),
      )
      .mockResolvedValueOnce(response([model]));
    expect(
      await resolveAccountModel(
        "openai-codex",
        "pacc_primary",
        model.id,
        new AbortController().signal,
      ),
    ).toMatchObject({
      model,
      accountRef: "pacc_primary",
      catalogRevision: response([]).data.catalogRevision,
      catalogDigest: response([]).data.catalogDigest,
      catalogStatus: catalogStatusFixture,
    });
    expect(sdk.listModelCapabilities.mock.lastCall?.[0]).toHaveProperty(
      "query.query",
      model.id,
    );
    expect(sdk.listModelCapabilities.mock.lastCall?.[0]).toHaveProperty(
      "query.pageToken",
      "next",
    );
  });
  it("не выдаёт другую модель при исчезновении выбранной", async () => {
    sdk.listModelCapabilities.mockResolvedValue(
      response([{ ...model, id: "other" }]),
    );
    expect(
      await resolveAccountModel(
        "openai-codex",
        "pacc_primary",
        model.id,
        new AbortController().signal,
      ),
    ).toMatchObject({ model: undefined, catalogStatus: catalogStatusFixture });
  });
  it("отклоняет чужой provider и повторяющийся cursor", async () => {
    sdk.listModelCapabilities.mockResolvedValue(
      response([{ ...model, providerDefinitionKey: "other" }]),
    );
    await expect(
      loadModelCatalog(
        "openai-codex",
        "pacc_primary",
        "",
        undefined,
        new AbortController().signal,
      ),
    ).rejects.toThrow("scope mismatch");
    sdk.listModelCapabilities.mockResolvedValue(response([], "same"));
    await expect(
      resolveAccountModel(
        "openai-codex",
        "pacc_primary",
        model.id,
        new AbortController().signal,
      ),
    ).rejects.toThrow("cursor repeated");
  });
  it("не начинает lookup после отмены и закрыто проверяет account eligibility", async () => {
    const controller = new AbortController();
    controller.abort();
    await expect(
      resolveAccountModel(
        "openai-codex",
        "pacc_primary",
        model.id,
        controller.signal,
      ),
    ).rejects.toThrow();
    expect(sdk.listModelCapabilities).not.toHaveBeenCalled();
    expect(accountModelAvailable(model, "pacc_primary")).toBe(true);
    expect(accountModelAvailable(model, "pacc_other")).toBe(false);
    expect(
      accountModelAvailable(
        { ...model, readinessBlockers: ["NOT_READY"] },
        "pacc_primary",
      ),
    ).toBe(false);
    expect(
      accountModelAvailable({ ...model, available: false }, "pacc_primary"),
    ).toBe(false);
  });
});
