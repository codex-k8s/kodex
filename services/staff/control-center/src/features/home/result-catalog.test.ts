import { beforeEach, describe, expect, it, vi } from "vitest";
const api = vi.hoisted(() => ({
  listRuns: vi.fn(),
  listArtifacts: vi.fn(),
  listOrganizationArtifacts: vi.fn(),
}));
vi.mock("@/shared/api/generated/openapi/sdk.gen", () => api);
vi.mock("@/shared/api/client", () => ({
  requestSignal: (signal: AbortSignal) => signal,
}));
vi.mock("@/shared/api/problem", () => ({
  unwrap: (value: unknown) => Promise.resolve(value),
}));
import { loadHomeResultPage, type HomeResultScope } from "./result-catalog";
const scope: HomeResultScope = {
  kind: "ARTIFACT",
  projectRef: "",
  query: "report",
  runFilter: "ACTIVE",
};
const artifact = {
  ref: "artifact-private",
  fileName: "report.txt",
  mediaType: "text/plain",
  scanState: "CLEAN",
  lifecycleState: "ACTIVE",
};
beforeEach(() => vi.clearAllMocks());
describe("Home: authoritative totals и общий каталог", () => {
  it("читает общий каталог один раз, сохраняет total и не выдумывает project route личного файла", async () => {
    api.listOrganizationArtifacts.mockResolvedValue({
      data: { items: [artifact], total: 52, nextPageToken: "next" },
    });
    const signal = new AbortController().signal;
    const page = await loadHomeResultPage(scope, undefined, signal);
    expect(page.total).toBe(52);
    expect(page.items[0]?.to).toBeUndefined();
    expect(api.listArtifacts).not.toHaveBeenCalled();
    expect(api.listOrganizationArtifacts).toHaveBeenCalledWith({
      query: {
        query: "report",
        pageToken: undefined,
        pageSize: 30,
        lifecycleState: "ACTIVE",
      },
      signal,
    });
  });
  it("передаёт project filter владельцу и отвергает чужую строку", async () => {
    api.listArtifacts.mockResolvedValue({
      data: { items: [{ ...artifact, projectRef: "other" }], total: 1 },
    });
    await expect(
      loadHomeResultPage(
        { ...scope, projectRef: "project-one" },
        undefined,
        new AbortController().signal,
      ),
    ).rejects.toThrow("scope mismatch");
    expect(api.listOrganizationArtifacts).not.toHaveBeenCalled();
  });
  it.each([-1, 0, 1.5, undefined])(
    "не подменяет неверный total %s длиной страницы",
    async (total) => {
      api.listOrganizationArtifacts.mockResolvedValue({
        data: { items: [artifact], total },
      });
      await expect(
        loadHomeResultPage(scope, undefined, new AbortController().signal),
      ).rejects.toThrow("Invalid Home");
    },
  );
  it("фильтрует terminal запрос на сервере и отклоняет active строку в ответе", async () => {
    api.listRuns.mockResolvedValue({
      data: {
        items: [{ ref: "run-one", title: "run", state: "RUNNING" }],
        total: 1,
      },
    });
    await expect(
      loadHomeResultPage(
        { ...scope, kind: "RUN", runFilter: "TERMINAL" },
        undefined,
        new AbortController().signal,
      ),
    ).rejects.toThrow("scope mismatch");
    const call = api.listRuns.mock.calls[0]?.[0] as {
      query: { states: string[] };
    };
    expect(call.query.states).toEqual(["SUCCEEDED", "FAILED", "CANCELLED"]);
  });
});
