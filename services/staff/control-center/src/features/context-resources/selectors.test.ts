import { beforeEach, describe, expect, it, vi } from "vitest";
const client = vi.hoisted(() => ({ get: vi.fn() }));
vi.mock("@/shared/api/generated/openapi/client.gen", () => ({ client }));
vi.mock("@/shared/api/client", () => ({
  requestSignal: (signal: AbortSignal) => signal,
}));
import { runs, sourceRun } from "./selectors";
describe("memory source selectors", () => {
  beforeEach(() => client.get.mockReset());
  it("передает project/query/cursor серверу и показывает результат без вывода authority", async () => {
    client.get.mockResolvedValue({
      response: new Response(null, { status: 200 }),
      data: {
        items: [
          {
            ref: "run_one",
            projectRef: "project_one",
            title: "Run",
            activitySummary: "Summary",
            state: "SUCCEEDED",
          },
        ],
        nextPageToken: "next",
      },
    });
    const signal = new AbortController().signal;
    expect(await runs("project_one", "needle", "cursor", signal)).toEqual({
      items: [
        {
          ref: "run_one",
          title: "Run",
          description: "Summary",
          meta: "SUCCEEDED",
        },
      ],
      nextPageToken: "next",
    });
    expect(client.get).toHaveBeenCalledWith(
      expect.objectContaining({
        url: "/api/v1/runs",
        query: {
          projectRef: "project_one",
          query: "needle",
          pageToken: "cursor",
          pageSize: 40,
        },
        signal,
      }),
    );
  });
  it("отклоняет чужой результат поиска и подмену exact run", async () => {
    client.get.mockResolvedValue({
      response: new Response(null, { status: 200 }),
      data: { items: [{ ref: "run_one", projectRef: "foreign" }] },
    });
    await expect(
      runs("project_one", "", undefined, new AbortController().signal),
    ).rejects.toThrow("scope");
    client.get.mockResolvedValue({
      response: new Response(null, { status: 200 }),
      data: { ref: "run_other", projectRef: "project_one" },
    });
    await expect(
      sourceRun("project_one", "run_one", new AbortController().signal),
    ).rejects.toThrow("scope");
  });
});
