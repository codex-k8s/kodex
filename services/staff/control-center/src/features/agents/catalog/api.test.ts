import { beforeEach, describe, expect, it, vi } from "vitest";

const client = vi.hoisted(() => ({ get: vi.fn() }));
vi.mock("@/shared/api/generated/openapi/client.gen", () => ({ client }));
vi.mock("@/shared/api/client", () => ({
  requestSignal: () => new AbortController().signal,
}));

import { loadAgentCatalogPage } from "./api";

function response<T>(data: T) {
  return Promise.resolve({
    data,
    error: undefined,
    response: new Response(null, { status: 200 }),
  });
}

describe("agent catalog API", () => {
  beforeEach(() => vi.clearAllMocks());

  it("передаёт авторитетному API project, нормализованный поиск и cursor", async () => {
    client.get.mockReturnValueOnce(
      response({ items: [], nextPageToken: "page_2" }),
    );

    await expect(
      loadAgentCatalogPage({
        projectRef: "project_sales",
        query: "  аналитик  ",
        pageToken: "page_1",
      }),
    ).resolves.toEqual({ items: [], nextPageToken: "page_2" });
    expect(client.get).toHaveBeenCalledWith(
      expect.objectContaining({
        url: "/api/v1/projects/{projectRef}/agents",
        path: { projectRef: "project_sales" },
        query: {
          pageSize: 40,
          query: "аналитик",
          pageToken: "page_1",
        },
      }),
    );
  });
});
