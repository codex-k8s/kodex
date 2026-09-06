import { beforeEach, describe, expect, it, vi } from "vitest";
const api = vi.hoisted(() => ({ listRuns: vi.fn() }));
vi.mock("@/shared/api/generated/openapi/sdk.gen", () => api);
vi.mock("@/shared/api/client", () => ({
  requestSignal: (signal: AbortSignal) => signal,
}));
vi.mock("@/shared/api/problem", () => ({
  unwrap: (value: unknown) => Promise.resolve(value),
}));
import { loadSessionCatalog } from "./session-catalog";
const item = {
  ref: "run-one",
  sessionRef: "session-one",
  projectRef: "project-one",
  state: "SUCCEEDED",
  nextActions: ["ADD_TURN"],
  target: { type: "AGENT", ref: "agent-one" },
};
beforeEach(() => vi.clearAllMocks());
describe("distinct Session owner catalog", () => {
  it("сохраняет distinct total и точный mode/target/cursor", async () => {
    api.listRuns.mockResolvedValue({
      data: { items: [item], total: 43, nextPageToken: "next" },
    });
    const signal = new AbortController().signal;
    const page = await loadSessionCatalog(
      {
        query: " review ",
        projectRef: "project-one",
        targetType: "AGENT",
        targetRef: "agent-one",
      },
      "first",
      signal,
    );
    expect(page.total).toBe(43);
    expect(api.listRuns).toHaveBeenCalledWith({
      query: {
        resumableSessionsOnly: true,
        projectRef: "project-one",
        query: "review",
        targetType: "AGENT",
        targetRef: "agent-one",
        pageToken: "first",
        pageSize: 30,
      },
      signal,
    });
  });
  it.each([
    { items: [item, { ...item, ref: "run-two" }], total: 2 },
    { items: [{ ...item, nextActions: [] }], total: 1 },
    { items: [{ ...item, projectRef: "foreign" }], total: 1 },
    {
      items: [{ ...item, target: { type: "AGENT", ref: "foreign" } }],
      total: 1,
    },
    { items: [item], total: undefined },
    { items: [item], total: 0 },
    { items: [item], total: 1, nextPageToken: "first" },
  ])(
    "не фильтрует повреждённую owner страницу до частичного успеха",
    async (page) => {
      api.listRuns.mockResolvedValue({ data: page });
      await expect(
        loadSessionCatalog(
          {
            query: "",
            projectRef: "project-one",
            targetType: "AGENT",
            targetRef: "agent-one",
          },
          "first",
          new AbortController().signal,
        ),
      ).rejects.toThrow("Invalid resumable");
      expect(api.listRuns).toHaveBeenCalledTimes(1);
    },
  );
  it("не отправляет неполный target и отбрасывает поздний ответ", async () => {
    await expect(
      loadSessionCatalog(
        { query: "", targetType: "AGENT" },
        undefined,
        new AbortController().signal,
      ),
    ).rejects.toThrow("Incomplete");
    expect(api.listRuns).not.toHaveBeenCalled();
    const controller = new AbortController();
    api.listRuns.mockImplementation(() => {
      controller.abort();
      return { data: { items: [], total: 0 } };
    });
    await expect(
      loadSessionCatalog({ query: "" }, undefined, controller.signal),
    ).rejects.toMatchObject({ name: "AbortError" });
  });
});
