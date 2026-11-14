import { describe, expect, it, vi } from "vitest";
import { defaultModelDiscoveryFilters } from "./model-discovery-filters";

const sdk = vi.hoisted(() => ({ listModelDiscovery: vi.fn() }));
vi.mock("@/shared/api/generated/openapi/sdk.gen", () => sdk);
vi.mock("@/shared/api/client", () => ({
  requestSignal: (signal: AbortSignal) => signal,
}));
vi.mock("@/shared/api/problem", () => ({
  unwrap: async (value: unknown) => await value,
}));
import { loadModelDiscovery } from "./model-discovery-api";

describe("API фильтров каталога моделей", () => {
  it("передаёт все типизированные фильтры и grouping без потери cursor", async () => {
    sdk.listModelDiscovery.mockResolvedValue({ data: { items: [] } });
    const signal = new AbortController().signal;
    await loadModelDiscovery(" glm ", "pinned", signal, {
      providerId: "zai-coding-plan",
      family: "glm",
      minimumContextTokens: 1000000,
      inputModality: "image",
      outputModality: "text",
      tools: "SUPPORTED",
      reasoning: "UNKNOWN",
      cache: "IMPLICIT",
      groupBy: "FAMILY",
    });
    expect(sdk.listModelDiscovery).toHaveBeenLastCalledWith({
      signal,
      query: {
        pageSize: 20,
        query: "glm",
        pageToken: "pinned",
        providerId: "zai-coding-plan",
        family: "glm",
        minimumContextTokens: 1000000,
        inputModality: "image",
        outputModality: "text",
        tools: "SUPPORTED",
        reasoning: "UNKNOWN",
        cache: "IMPLICIT",
        groupBy: "FAMILY",
      },
    });
  });
  it("отклоняет дробный размер контекста до SDK", async () => {
    sdk.listModelDiscovery.mockClear();
    await expect(
      loadModelDiscovery("", "", new AbortController().signal, {
        ...defaultModelDiscoveryFilters(),
        minimumContextTokens: 0.5,
      }),
    ).rejects.toThrow();
    expect(sdk.listModelDiscovery).not.toHaveBeenCalled();
  });
});
