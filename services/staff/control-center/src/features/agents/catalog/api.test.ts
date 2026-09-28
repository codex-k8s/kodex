import { beforeEach, describe, expect, it, vi } from "vitest";

const client = vi.hoisted(() => ({ get: vi.fn() }));
vi.mock("@/shared/api/generated/openapi/client.gen", () => ({ client }));
vi.mock("@/shared/api/client", () => ({
  requestSignal: (signal?: AbortSignal) =>
    signal ?? new AbortController().signal,
}));

import { loadAgentCatalogPage, loadAssignedAgent } from "./api";

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
          pageSize: 20,
          query: "аналитик",
          pageToken: "page_1",
        },
      }),
    );
  });
  it("передаёт отмену поиска из async picker в generated client", async () => {
    client.get.mockReturnValueOnce(response({ items: [], nextPageToken: "" }));
    const controller = new AbortController();
    await loadAgentCatalogPage(
      { projectRef: "project_sales", query: "" },
      controller.signal,
    );
    expect(client.get).toHaveBeenCalledWith(
      expect.objectContaining({ signal: controller.signal }),
    );
  });

  it("адресно читает сохранённого исполнителя и проверяет Проект", async () => {
    const controller = new AbortController();
    const agent = {
      ref: "agent_101",
      projectRef: "project_sales",
      name: "Аналитик",
    };
    client.get.mockReturnValueOnce(response(agent));
    await expect(
      loadAssignedAgent("project_sales", agent.ref, controller.signal),
    ).resolves.toEqual(agent);
    expect(client.get).toHaveBeenCalledWith(
      expect.objectContaining({
        url: "/api/v1/agents/{agentRef}",
        path: { agentRef: agent.ref },
        signal: controller.signal,
        cache: "no-store",
      }),
    );

    client.get.mockReturnValueOnce(
      response({ ...agent, projectRef: "project_other" }),
    );
    await expect(
      loadAssignedAgent("project_sales", agent.ref, controller.signal),
    ).rejects.toThrow("scope mismatch");
  });
});
